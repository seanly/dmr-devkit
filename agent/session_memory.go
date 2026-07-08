package agent

import (
	"fmt"
	"strings"
	"sync"
)

// memorySegmentType classifies a captured conversation moment.
type memorySegmentType string

const (
	segUserIntent memorySegmentType = "user_intent"
	segToolResult memorySegmentType = "tool_result"
	segFileChange memorySegmentType = "file_change"
	segError      memorySegmentType = "error"
	segDecision   memorySegmentType = "decision"
)

// memorySegment is a single locally-extracted conversation moment. It carries
// no LLM-generated prose; segments are produced by lightweight rules so that a
// compact can assemble a summary without an extra model call.
type memorySegment struct {
	Step    int
	Type    memorySegmentType
	Content string
	Tokens  int
}

// SessionMemory maintains an incrementally-built, rule-extracted summary of a
// conversation. When a compact is triggered, the assembled memory can serve as
// the compact summary directly, avoiding an LLM call in the common case.
//
// Segments are appended at the end of each conversation turn. The memory is
// pure-local: no model calls. When the assembled text is too short to be
// useful, the caller falls back to LLM summarization (SessionMemoryFallback).
type SessionMemory struct {
	mu       sync.Mutex
	segments []memorySegment
	lastAssembledAtStep int
}

func newSessionMemory() *SessionMemory {
	return &SessionMemory{}
}

// AppendSegment adds a locally-extracted segment. It is safe for concurrent use.
func (m *SessionMemory) AppendSegment(s memorySegment) {
	if m == nil {
		return
	}
	if strings.TrimSpace(s.Content) == "" {
		return
	}
	if s.Tokens <= 0 {
		s.Tokens = approxTokens(s.Content)
	}
	m.mu.Lock()
	m.segments = append(m.segments, s)
	m.mu.Unlock()
}

// Reset clears all segments (used after a successful compact so the next cycle
// starts fresh).
func (m *SessionMemory) Reset() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.segments = nil
	m.lastAssembledAtStep = 0
	m.mu.Unlock()
}

// SegmentsSince returns segments captured after the given step (exclusive).
// Pass 0 to get all segments.
func (m *SessionMemory) SegmentsSince(afterStep int) []memorySegment {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if afterStep <= 0 {
		out := make([]memorySegment, len(m.segments))
		copy(out, m.segments)
		return out
	}
	var out []memorySegment
	for _, s := range m.segments {
		if s.Step > afterStep {
			out = append(out, s)
		}
	}
	return out
}

// TotalTokens sums the estimated tokens across all segments.
func (m *SessionMemory) TotalTokens() int {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	total := 0
	for _, s := range m.segments {
		total += s.Tokens
	}
	return total
}

// IsEmpty reports whether no segments have been captured.
func (m *SessionMemory) IsEmpty() bool {
	if m == nil {
		return true
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.segments) == 0
}

// assembleSummary builds a structured (9-section style) summary from the
// captured segments without an LLM call. It returns the assembled text and the
// estimated token count. When the segments are too sparse (< minChars), it
// returns ("", 0) so the caller can fall back to LLM summarization.
//
// When the assembled text exceeds maxChars, older segments within each category
// are compressed: only the most recent `keepPerType` are retained verbatim and
// the rest are collapsed into a count.
func (m *SessionMemory) assembleSummary(minChars, maxChars, keepPerType int) (string, int) {
	if m == nil {
		return "", 0
	}
	if keepPerType <= 0 {
		keepPerType = 3
	}
	m.mu.Lock()
	segs := make([]memorySegment, len(m.segments))
	copy(segs, m.segments)
	m.mu.Unlock()
	if len(segs) == 0 {
		return "", 0
	}

	// Group segments by type, preserving insertion order.
	byType := map[memorySegmentType][]memorySegment{}
	var order []memorySegmentType
	for _, s := range segs {
		if _, ok := byType[s.Type]; !ok {
			order = append(order, s.Type)
		}
		byType[s.Type] = append(byType[s.Type], s)
	}

	var b strings.Builder
	b.WriteString("[Context Summary]\n")
	for _, t := range orderedTypes(order) {
		items := byType[t]
		if len(items) == 0 {
			continue
		}
		header := sectionHeader(t)
		if header == "" {
			continue
		}
		b.WriteString("\n")
		b.WriteString(header)
		b.WriteString("\n")

		// If this category alone would blow the budget, keep only the most
		// recent keepPerType and summarize the rest as a count.
		if categoryChars(items) > maxChars/2 && len(items) > keepPerType {
			kept := items[len(items)-keepPerType:]
			dropped := len(items) - keepPerType
			for _, it := range kept {
				b.WriteString("- ")
				b.WriteString(it.Content)
				b.WriteString("\n")
			}
			b.WriteString(fmt.Sprintf("- (... %d earlier %s entries omitted)\n", dropped, t))
			continue
		}
		for _, it := range items {
			b.WriteString("- ")
			b.WriteString(it.Content)
			b.WriteString("\n")
		}
	}

	text := strings.TrimSpace(b.String())
	if len(text) < minChars {
		return "", 0
	}
	return text, approxTokens(text)
}

// orderedTypes returns the canonical section order, with any extra types appended.
func orderedTypes(seen []memorySegmentType) []memorySegmentType {
	canonical := []memorySegmentType{
		segUserIntent, segDecision, segFileChange, segToolResult, segError,
	}
	out := make([]memorySegmentType, 0, len(canonical))
	seenSet := map[memorySegmentType]bool{}
	for _, t := range canonical {
		out = append(out, t)
		seenSet[t] = true
	}
	for _, t := range seen {
		if !seenSet[t] {
			out = append(out, t)
		}
	}
	return out
}

func sectionHeader(t memorySegmentType) string {
	switch t {
	case segUserIntent:
		return "1. Primary Request and Intent:"
	case segDecision:
		return "5. Problem Solving / Decisions:"
	case segFileChange:
		return "3. Files and Code Sections:"
	case segToolResult:
		return "Technical Activity (tool results):"
	case segError:
		return "4. Errors and Fixes:"
	default:
		return ""
	}
}

func categoryChars(items []memorySegment) int {
	n := 0
	for _, it := range items {
		n += len(it.Content)
	}
	return n
}

// approxTokens is a cheap rune-based token estimate (≈ chars/4 for non-CJK,
// ≈ chars/1.5 for CJK). It mirrors the TokenEstimator heuristic without its
// per-message overhead, which is fine for budget decisions in SessionMemory.
func approxTokens(s string) int {
	if s == "" {
		return 0
	}
	runes := 0
	cjk := 0
	for _, r := range s {
		runes++
		if r >= '一' && r <= '鿿' {
			cjk++
		}
	}
	if runes > 0 && cjk*100/runes > 30 {
		return int(float64(runes) / 1.5)
	}
	return runes / 4
}

// extractSegmentsFromTurn produces memory segments from one conversation turn's
// messages. It is a pure function over the message maps produced by the loop;
// no tape or LLM access is required.
//
//   - user messages become segUserIntent (truncated)
//   - assistant text becomes segDecision (truncated reasoning)
//   - tool_call/tool messages become segToolResult or segFileChange/segError
//     depending on the tool name and content
func extractSegmentsFromTurn(step int, messages []map[string]any) []memorySegment {
	var out []memorySegment
	for _, msg := range messages {
		role, _ := msg["role"].(string)
		content := JoinedContent(msg)
		switch role {
		case "user":
			c := strings.TrimSpace(content)
			if c == "" {
				continue
			}
			out = append(out, memorySegment{Step: step, Type: segUserIntent, Content: truncateRunes(c, 200)})
		case "assistant":
			c := strings.TrimSpace(content)
			if c == "" {
				continue
			}
			out = append(out, memorySegment{Step: step, Type: segDecision, Content: truncateRunes(c, 200)})
		case "tool":
			toolName, _ := msg["tool_name"].(string)
			if toolName == "" {
				toolName = "tool"
			}
			c := strings.TrimSpace(content)
			if c == "" {
				continue
			}
			seg := memorySegment{Step: step, Type: segToolResult, Content: fmt.Sprintf("%s: %s", toolName, truncateRunes(c, 300))}
			if isWriteTool(toolName) {
				seg.Type = segFileChange
				seg.Content = fmt.Sprintf("%s modified file: %s", toolName, truncateRunes(c, 200))
			}
			if looksLikeError(c) {
				seg.Type = segError
				seg.Content = fmt.Sprintf("%s error: %s", toolName, truncateRunes(c, 300))
			}
			out = append(out, seg)
		}
	}
	return out
}

var writeToolNames = map[string]bool{
	"write_file": true, "edit_file": true, "write": true, "edit": true,
	"create_file": true, "apply_patch": true, "str_replace_editor": true,
}

func isWriteTool(name string) bool {
	return writeToolNames[strings.ToLower(name)]
}

func looksLikeError(content string) bool {
	lower := strings.ToLower(content)
	if strings.HasPrefix(lower, "error") || strings.Contains(lower, "error:") {
		return true
	}
	if strings.Contains(lower, "failed") || strings.Contains(lower, "not found") {
		return true
	}
	if strings.Contains(lower, "exception") || strings.Contains(lower, "traceback") {
		return true
	}
	return false
}

// minSessionMemoryChars is the floor below which the assembled memory is
// considered too sparse to use as a compact summary.
const minSessionMemoryChars = 100

// maxSessionMemoryChars is the ceiling above which older segments are
// compressed to keep the summary bounded.
const maxSessionMemoryChars = 8000
