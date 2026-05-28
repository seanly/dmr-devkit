package toolresult

import (
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	tapepkg "github.com/seanly/dmr-devkit/tape"
	"github.com/seanly/dmr-devkit/tool"
)

// TapeState tracks tool_call ids that have crossed the agent loop's budget logic.
type TapeState struct {
	Seen         map[string]struct{}
	Replacements map[string]string // tool_call_id -> exact persisted-output string
}

func newTapeState() *TapeState {
	return &TapeState{
		Seen:         map[string]struct{}{},
		Replacements: map[string]string{},
	}
}

func cloneTapeState(src *TapeState) *TapeState {
	if src == nil {
		return newTapeState()
	}
	out := newTapeState()
	for k := range src.Seen {
		out.Seen[k] = struct{}{}
	}
	for k, v := range src.Replacements {
		out.Replacements[k] = v
	}
	return out
}

// Manager holds per-tape replacement state across turns.
type Manager struct {
	policy Policy
	mu     sync.Mutex
	states map[string]*TapeState
	mcLastAssist map[string]int64 // unix seconds; gap-based microcompact
}

// NewManager returns a manager using the given workspace-backed policy.
func NewManager(p Policy) *Manager {
	if p.SkipTools == nil {
		p.SkipTools = map[string]struct{}{"fsRead": {}}
	}
	return &Manager{
		policy:       p,
		states:       map[string]*TapeState{},
		mcLastAssist: map[string]int64{},
	}
}

// CloneState returns a sibling manager with duplicated per-tape state (subagent forks).
func (m *Manager) CloneState() *Manager {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := NewManager(m.policy)
	for k, v := range m.states {
		out.states[k] = cloneTapeState(v)
	}
	for k, v := range m.mcLastAssist {
		out.mcLastAssist[k] = v
	}
	return out
}

func (m *Manager) lockedState(tape string) *TapeState {
	st := m.states[tape]
	if st == nil {
		st = newTapeState()
		m.states[tape] = st
	}
	return st
}

func (m *Manager) mergeFlatMessagesLocked(tape string, messages []map[string]any) {
	if tape == "" {
		return
	}
	st := m.lockedState(tape)
	for _, msg := range messages {
		if r, _ := msg["role"].(string); r != "tool" {
			continue
		}
		id, _ := msg["tool_call_id"].(string)
		if id == "" {
			continue
		}
		raw, _ := msg["content"].(string)
		st.Seen[id] = struct{}{}
		if strings.HasPrefix(strings.TrimSpace(raw), PersistedOutputTag) {
			st.Replacements[id] = raw
		}
	}
}

// MergeFlatMessages updates persisted/replay bookkeeping from flattened LLM transcript.
func (m *Manager) MergeFlatMessages(tape string, messages []map[string]any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mergeFlatMessagesLocked(tape, messages)
}

// NoteAssistantTurn records assistant message time for microcompact gap heuristic.
func (m *Manager) NoteAssistantTurn(tape string, t time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mcLastAssist[tape] = t.Unix()
}

// PrepareWireMessages applies merge + microcompact transform before an LLM request.
func (m *Manager) PrepareWireMessages(tape string, messages []map[string]any, now time.Time) {
	if tape == "" || len(messages) == 0 {
		return
	}
	m.MergeFlatMessages(tape, messages)
	m.applyMicrocompact(tape, messages, now)
}

// ProcessNew applies normalization, optional persistence, before aggregate budget.
func (m *Manager) ProcessNew(threshold int, tapeName, toolCallID, toolName string, raw any) string {
	text := tapepkg.FormatToolResultItem(raw)
	text = NormalizeEmpty(text, toolName)
	th := threshold
	if m.policy.skips(toolName) {
		th = -1
	}
	if th < 0 {
		return text
	}
	if utf8.RuneCountInString(text) <= th {
		return text
	}
	msg, _, err := m.policy.persistAndBuildMessage(m.policy.Workspace, tapeName, toolCallID, text)
	if err != nil {
		slog.Warn("toolresult: persist failed, truncating", "tape", tapeName, "tool", toolName, "error", err)
		return TruncateWithHint(text, th)
	}
	return msg
}

// ContentReplacement records a budget substitution for optional tape audit entries.
type ContentReplacement struct {
	ToolCallID  string
	Replacement string
}

// ApplyTurnBudget rewrites recent tool rows when aggregate size exceeds budget.
// msgs layout: msgs[0] assistant with tool_calls, msgs[1:] role=tool in call order.
func (m *Manager) ApplyTurnBudget(tape string, msgs []map[string]any) []ContentReplacement {
	if tape == "" || len(msgs) < 2 {
		return nil
	}
	limit := m.policy.effectiveBudget()

	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.lockedState(tape)

	type cand struct {
		idx int
		id  string
		sz  int
	}

	var fresh []cand
	frozenSize := 0

	for idx := 1; idx < len(msgs); idx++ {
		msg := msgs[idx]
		if msg == nil || msg["role"] != "tool" {
			continue
		}
		id, _ := msg["tool_call_id"].(string)
		if id == "" {
			continue
		}
		tname := toolNameForToolMessage(msgs, idx)
		raw, _ := msg["content"].(string)
		content := NormalizeEmpty(strings.TrimSpace(raw), tname)

		if repl, ok := st.Replacements[id]; ok {
			msg["content"] = repl
			st.Seen[id] = struct{}{}
			frozenSize += utf8.RuneCountInString(repl)
			continue
		}
		if _, seen := st.Seen[id]; seen {
			frozenSize += utf8.RuneCountInString(content)
			continue
		}

		if m.policy.skips(tname) {
			st.Seen[id] = struct{}{}
			continue
		}

		sz := utf8.RuneCountInString(content)
		msg["content"] = content
		fresh = append(fresh, cand{idx: idx, id: id, sz: sz})
	}

	if len(fresh) == 0 {
		return nil
	}

	freshSize := 0
	for _, c := range fresh {
		freshSize += c.sz
	}
	total := frozenSize + freshSize
	if total <= limit {
		for _, c := range fresh {
			st.Seen[c.id] = struct{}{}
		}
		return nil
	}

	sort.Slice(fresh, func(i, j int) bool {
		return fresh[i].sz > fresh[j].sz
	})

	remaining := total
	var toPersist []cand
	for _, c := range fresh {
		if remaining <= limit {
			break
		}
		toPersist = append(toPersist, c)
		remaining -= c.sz
	}

	var out []ContentReplacement
	for _, c := range toPersist {
		txt, _ := msgs[c.idx]["content"].(string)
		subst, _, err := m.policy.persistAndBuildMessage(m.policy.Workspace, tape, c.id, txt)
		if err != nil {
			subst = TruncateWithHint(txt, m.policy.effectiveMaxChars())
		}
		msgs[c.idx]["content"] = subst
		st.Replacements[c.id] = subst
		st.Seen[c.id] = struct{}{}
		out = append(out, ContentReplacement{ToolCallID: c.id, Replacement: subst})
	}

	for _, c := range fresh {
		if _, done := st.Seen[c.id]; !done {
			st.Seen[c.id] = struct{}{}
		}
	}
	return out
}

func toolNameForToolMessage(msgs []map[string]any, toolIdx int) string {
	callID, _ := msgs[toolIdx]["tool_call_id"].(string)
	for i := toolIdx - 1; i >= 0; i-- {
		if msgs[i]["role"] != "assistant" {
			continue
		}
		tcs, _ := msgs[i]["tool_calls"].([]any)
		for _, tc := range tcs {
			tcm, _ := tc.(map[string]any)
			if tcm == nil {
				continue
			}
			cid, _ := tcm["id"].(string)
			if cid != "" && cid == callID {
				fn, _ := tcm["function"].(map[string]any)
				if fn == nil {
					return ""
				}
				name, _ := fn["name"].(string)
				return name
			}
		}
	}
	return ""
}

func (m *Manager) applyMicrocompact(tape string, msgs []map[string]any, now time.Time) {
	mc := m.policy.Microcompact
	if !mc.Enabled || tape == "" {
		return
	}
	toolsMap := mc.CompactableTools
	if len(toolsMap) == 0 {
		toolsMap = DefaultMicrocompactTools()
	}

	m.mu.Lock()
	gapTriggered := mc.GapMinutes > 0 && m.gapTriggeredLocked(tape, now)
	m.mu.Unlock()

	idxs := compactableToolIndexes(msgs, toolsMap)
	if len(idxs) == 0 {
		return
	}
	keep := mc.KeepRecent
	if keep < 1 {
		keep = 1
	}
	if gapTriggered {
		clearAllButLastN(idxs, keep, msgs)
		return
	}
	if len(idxs) <= keep {
		return
	}
	clearCount := len(idxs) - keep
	toClear := map[int]struct{}{}
	for i := 0; i < clearCount; i++ {
		toClear[idxs[i]] = struct{}{}
	}
	clearToolMessagesAt(msgs, toClear)
}

func (m *Manager) gapTriggeredLocked(tape string, now time.Time) bool {
	last, ok := m.mcLastAssist[tape]
	if !ok || last <= 0 {
		return false
	}
	gapMin := m.policy.Microcompact.GapMinutes
	if gapMin <= 0 {
		return false
	}
	delta := now.Sub(time.Unix(last, 0)).Minutes()
	return delta >= gapMin
}

func compactableToolIndexes(msgs []map[string]any, names map[string]struct{}) []int {
	var out []int
	for i := 0; i < len(msgs); i++ {
		msg := msgs[i]
		if msg == nil || msg["role"] != "tool" {
			continue
		}
		tname := toolNameForToolMessage(msgs, i)
		if _, ok := names[tname]; !ok {
			continue
		}
		raw, _ := msg["content"].(string)
		raw = strings.TrimSpace(raw)
		if raw == ToolResultClearedMessage {
			continue
		}
		if strings.HasPrefix(raw, PersistedOutputTag) {
			continue
		}
		out = append(out, i)
	}
	return out
}

func clearAllButLastN(idxs []int, keep int, msgs []map[string]any) {
	if len(idxs) <= keep {
		return
	}
	clear := map[int]struct{}{}
	for i := 0; i < len(idxs)-keep; i++ {
		clear[idxs[i]] = struct{}{}
	}
	clearToolMessagesAt(msgs, clear)
}

func clearToolMessagesAt(msgs []map[string]any, idx map[int]struct{}) {
	for i := range idx {
		if i < 0 || i >= len(msgs) {
			continue
		}
		if msgs[i] == nil {
			continue
		}
		if msgs[i]["role"] != "tool" {
			continue
		}
		msgs[i]["content"] = ToolResultClearedMessage
	}
}

// EffectiveThreshold resolves the externalize cap for one tool invocation.
func (m *Manager) EffectiveThreshold(tool *tool.Tool, configured int, toolName string) int {
	return EffectivePersistThreshold(tool, configured, m.policy, toolName)
}

