package agent

import (
	"strings"
	"testing"
)

func TestSessionMemory_AssembleAndFallback(t *testing.T) {
	m := newSessionMemory()

	// Empty memory → not usable.
	s, _ := m.assembleSummary(minSessionMemoryChars, maxSessionMemoryChars, 3)
	if s != "" {
		t.Fatalf("empty memory should not assemble, got s=%q", s)
	}

	m.AppendSegment(memorySegment{Step: 1, Type: segUserIntent, Content: "Build a compact coordinator for the agent loop"})
	m.AppendSegment(memorySegment{Step: 2, Type: segFileChange, Content: "edit_file: agent/loop.go"})
	m.AppendSegment(memorySegment{Step: 3, Type: segToolResult, Content: "read_file: agent/compact.go returned 322 lines"})
	m.AppendSegment(memorySegment{Step: 4, Type: segError, Content: "build failed: undefined applyProgressiveTrim"})
	m.AppendSegment(memorySegment{Step: 5, Type: segDecision, Content: "Decided to route all compact triggers through the coordinator"})

	summary, tokens := m.assembleSummary(minSessionMemoryChars, maxSessionMemoryChars, 3)
	if summary == "" {
		t.Fatal("non-empty memory should assemble into a usable summary")
	}
	if !strings.Contains(summary, "Primary Request and Intent") {
		t.Errorf("summary should contain intent section:\n%s", summary)
	}
	if !strings.Contains(summary, "Build a compact coordinator") {
		t.Errorf("summary should contain the user intent:\n%s", summary)
	}
	if !strings.Contains(summary, "Errors and Fixes") {
		t.Errorf("summary should contain errors section:\n%s", summary)
	}
	if tokens <= 0 {
		t.Errorf("token estimate should be positive, got %d", tokens)
	}

	// Too-sparse memory falls back.
	m2 := newSessionMemory()
	m2.AppendSegment(memorySegment{Step: 1, Type: segUserIntent, Content: "hi"})
	if s, _ := m2.assembleSummary(minSessionMemoryChars, maxSessionMemoryChars, 3); s != "" {
		t.Fatal("sparse memory should signal fallback (empty summary)")
	}
}

func TestSessionMemory_CompressesOldSegments(t *testing.T) {
	m := newSessionMemory()
	// Many tool-result segments beyond the per-type budget.
	for i := 0; i < 20; i++ {
		m.AppendSegment(memorySegment{Step: i, Type: segToolResult, Content: strings.Repeat("result-", 40) + "x"})
	}
	summary, _ := m.assembleSummary(minSessionMemoryChars, 4000, 3)
	if summary == "" {
		t.Fatal("should assemble")
	}
	if !strings.Contains(summary, "omitted") {
		t.Errorf("older segments should be compressed into a count, got:\n%s", summary)
	}
}

func TestSessionMemory_Reset(t *testing.T) {
	m := newSessionMemory()
	m.AppendSegment(memorySegment{Step: 1, Type: segUserIntent, Content: "do something meaningful here"})
	if m.IsEmpty() {
		t.Fatal("memory should not be empty after append")
	}
	m.Reset()
	if !m.IsEmpty() {
		t.Fatal("memory should be empty after reset")
	}
}

func TestExtractSegmentsFromTurn_Classification(t *testing.T) {
	msgs := []map[string]any{
		{"role": "user", "content": "Please refactor the compact pipeline"},
		{"role": "assistant", "content": "I'll start by reading the loop"},
		{"role": "tool", "tool_name": "edit_file", "content": "wrote agent/loop.go"},
		{"role": "tool", "tool_name": "bash", "content": "error: build failed"},
		{"role": "tool", "tool_name": "read_file", "content": "agent/compact.go"},
	}
	segs := extractSegmentsFromTurn(1, msgs)
	if len(segs) != 5 {
		t.Fatalf("expected 5 segments, got %d", len(segs))
	}

	types := map[memorySegmentType]bool{}
	for _, s := range segs {
		types[s.Type] = true
	}
	if !types[segUserIntent] || !types[segDecision] || !types[segFileChange] || !types[segError] || !types[segToolResult] {
		t.Errorf("classification incomplete: %+v", types)
	}

	// Verify write-tool and error classification landed on the right segments.
	var fileSeg, errSeg *memorySegment
	for i := range segs {
		if segs[i].Type == segFileChange {
			fileSeg = &segs[i]
		}
		if segs[i].Type == segError {
			errSeg = &segs[i]
		}
	}
	if fileSeg == nil || !strings.Contains(fileSeg.Content, "edit_file") {
		t.Errorf("edit_file should be a file_change segment, got %+v", fileSeg)
	}
	if errSeg == nil || !strings.Contains(errSeg.Content, "build failed") {
		t.Errorf("bash error should be an error segment, got %+v", errSeg)
	}
}

func TestSessionMemory_AppendSkipsEmpty(t *testing.T) {
	m := newSessionMemory()
	m.AppendSegment(memorySegment{Step: 1, Type: segUserIntent, Content: "   "})
	if !m.IsEmpty() {
		t.Fatal("whitespace-only segment should be ignored")
	}
}
