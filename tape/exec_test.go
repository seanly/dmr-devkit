package tape

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Basic execution tracking
// ---------------------------------------------------------------------------

func TestTapeController_RecordExecAndReplay(t *testing.T) {
	store := NewInMemoryTapeStore()
	tc := NewTapeController(NewTapeManager(store))

	// Simulate a complete execution
	tc.RecordExecStart("conv:1", "exec-1", "planner", map[string]any{"temperature": 0.7})
	tc.RecordExecInput("conv:1", "exec-1", []map[string]any{
		{"role": "user", "content": "hello"},
	})
	tc.RecordExecOutput("conv:1", "exec-1", []map[string]any{
		{"role": "assistant", "content": "hi there"},
	})
	tc.RecordExecState("conv:1", "exec-1", ExecStateCompleted)

	// Replay
	replay, err := tc.ReplayExec("conv:1", "exec-1")
	if err != nil {
		t.Fatal(err)
	}
	if replay.ExecID != "exec-1" {
		t.Errorf("ExecID = %q, want exec-1", replay.ExecID)
	}
	if replay.AgentID != "planner" {
		t.Errorf("AgentID = %q, want planner", replay.AgentID)
	}
	if replay.State != ExecStateCompleted {
		t.Errorf("State = %q, want completed", replay.State)
	}
	if len(replay.Inputs) != 1 {
		t.Errorf("Inputs len = %d, want 1", len(replay.Inputs))
	}
	if len(replay.Outputs) != 1 {
		t.Errorf("Outputs len = %d, want 1", len(replay.Outputs))
	}
	if len(replay.Messages) != 2 {
		t.Errorf("Messages len = %d, want 2", len(replay.Messages))
	}
	if replay.Messages[0]["content"] != "hello" {
		t.Errorf("first message = %v", replay.Messages[0])
	}
	if replay.Messages[1]["content"] != "hi there" {
		t.Errorf("second message = %v", replay.Messages[1])
	}
}

func TestTapeController_ReplayExecSubTapeMode(t *testing.T) {
	store := NewInMemoryTapeStore()
	tc := NewTapeController(NewTapeManager(store))

	// Sub-tape isolation: all entries belong to one exec
	tc.RecordExecStart("conv:1:exec:a", "exec-a", "coder", nil)
	tc.RecordExecInput("conv:1:exec:a", "exec-a", []map[string]any{
		{"role": "user", "content": "write a test"},
	})
	tc.RecordExecState("conv:1:exec:a", "exec-a", ExecStateCompleted)

	// Replay without execID filter (sub-tape mode)
	replay, err := tc.ReplayExec("conv:1:exec:a", "")
	if err != nil {
		t.Fatal(err)
	}
	if replay.AgentID != "coder" {
		t.Errorf("AgentID = %q, want coder", replay.AgentID)
	}
	if len(replay.Inputs) != 1 {
		t.Errorf("Inputs len = %d, want 1", len(replay.Inputs))
	}
}

func TestTapeController_ReplayExecWithExecIDFilter(t *testing.T) {
	store := NewInMemoryTapeStore()
	tc := NewTapeController(NewTapeManager(store))

	// Multiple execs in the same tape
	tc.RecordExecStart("conv:1", "exec-a", "planner", nil)
	tc.RecordExecInput("conv:1", "exec-a", []map[string]any{{"role": "user", "content": "task a"}})
	tc.RecordExecState("conv:1", "exec-a", ExecStateCompleted)

	tc.RecordExecStart("conv:1", "exec-b", "coder", nil)
	tc.RecordExecInput("conv:1", "exec-b", []map[string]any{{"role": "user", "content": "task b"}})
	tc.RecordExecState("conv:1", "exec-b", ExecStatePending)

	// Replay exec-b only
	replay, err := tc.ReplayExec("conv:1", "exec-b")
	if err != nil {
		t.Fatal(err)
	}
	if replay.AgentID != "coder" {
		t.Errorf("AgentID = %q, want coder", replay.AgentID)
	}
	if replay.State != ExecStatePending {
		t.Errorf("State = %q, want pending", replay.State)
	}
	if len(replay.Inputs) != 1 || replay.Inputs[0]["content"] != "task b" {
		t.Errorf("Inputs = %v", replay.Inputs)
	}
}

// ---------------------------------------------------------------------------
// Pending execution detection
// ---------------------------------------------------------------------------

func TestTapeController_FindPendingExec(t *testing.T) {
	store := NewInMemoryTapeStore()
	tc := NewTapeController(NewTapeManager(store))

	// First exec completed
	tc.RecordExecStart("conv:1", "exec-1", "planner", nil)
	tc.RecordExecState("conv:1", "exec-1", ExecStateCompleted)

	// Second exec pending
	tc.RecordExecStart("conv:1", "exec-2", "planner", nil)
	tc.RecordExecState("conv:1", "exec-2", ExecStatePending)

	pending, err := tc.FindPendingExec("conv:1")
	if err != nil {
		t.Fatal(err)
	}
	if pending != "exec-2" {
		t.Errorf("pending exec = %q, want exec-2", pending)
	}
}

func TestTapeController_FindPendingExecNone(t *testing.T) {
	store := NewInMemoryTapeStore()
	tc := NewTapeController(NewTapeManager(store))

	tc.RecordExecStart("conv:1", "exec-1", "planner", nil)
	tc.RecordExecState("conv:1", "exec-1", ExecStateCompleted)

	pending, err := tc.FindPendingExec("conv:1")
	if err != nil {
		t.Fatal(err)
	}
	if pending != "" {
		t.Errorf("pending exec = %q, want empty", pending)
	}
}

func TestTapeController_FindPendingExecResumedToCompleted(t *testing.T) {
	store := NewInMemoryTapeStore()
	tc := NewTapeController(NewTapeManager(store))

	// Exec starts pending, later completed
	tc.RecordExecStart("conv:1", "exec-1", "planner", nil)
	tc.RecordExecState("conv:1", "exec-1", ExecStatePending)
	tc.RecordExecOutput("conv:1", "exec-1", []map[string]any{{"role": "assistant", "content": "done"}})
	tc.RecordExecState("conv:1", "exec-1", ExecStateCompleted)

	pending, err := tc.FindPendingExec("conv:1")
	if err != nil {
		t.Fatal(err)
	}
	if pending != "" {
		t.Errorf("pending exec = %q, want empty (was completed)", pending)
	}
}

// ---------------------------------------------------------------------------
// Fork
// ---------------------------------------------------------------------------

func TestTapeController_Fork(t *testing.T) {
	store := NewInMemoryTapeStore()
	tc := NewTapeController(NewTapeManager(store))

	// Seed source tape
	_ = store.Append("conv:src", NewMessageEntry(map[string]any{"role": "user", "content": "msg1"}))
	_ = store.Append("conv:src", NewMessageEntry(map[string]any{"role": "user", "content": "msg2"}))
	_ = store.Append("conv:src", NewMessageEntry(map[string]any{"role": "user", "content": "msg3"}))

	// Fork at ID=1 (keep entries 0 and 1)
	err := tc.Fork("conv:src", 1, "conv:dst")
	if err != nil {
		t.Fatal(err)
	}

	entries, _ := store.FetchAll("conv:dst", nil)
	// 2 copied entries + 1 fork entry = 3
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if entries[0].Payload["content"] != "msg1" {
		t.Errorf("entry 0 content = %v", entries[0].Payload["content"])
	}
	if entries[1].Payload["content"] != "msg2" {
		t.Errorf("entry 1 content = %v", entries[1].Payload["content"])
	}
	if entries[2].Kind != "fork" {
		t.Errorf("entry 2 kind = %q, want fork", entries[2].Kind)
	}
}

func TestTapeController_ForkEmptyTape(t *testing.T) {
	store := NewInMemoryTapeStore()
	tc := NewTapeController(NewTapeManager(store))

	err := tc.Fork("conv:src", 0, "conv:dst")
	if err != nil {
		t.Fatal(err)
	}

	entries, _ := store.FetchAll("conv:dst", nil)
	if len(entries) != 1 {
		t.Fatalf("expected 1 fork entry, got %d", len(entries))
	}
}

// ---------------------------------------------------------------------------
// CatchUp (last_seq equivalent)
// ---------------------------------------------------------------------------

func TestTapeController_CatchUp(t *testing.T) {
	store := NewInMemoryTapeStore()
	tc := NewTapeController(NewTapeManager(store))

	for i := range 5 {
		_ = store.Append("conv:1", NewMessageEntry(map[string]any{"role": "user", "content": i}))
	}

	var caught []int
	err := tc.CatchUp("conv:1", 2, func(e TapeEntry) error {
		caught = append(caught, e.ID)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// IDs > 2: 3, 4
	if len(caught) != 2 {
		t.Fatalf("expected 2 caught entries, got %d", len(caught))
	}
	if caught[0] != 3 || caught[1] != 4 {
		t.Errorf("caught IDs = %v, want [3 4]", caught)
	}
}

// ---------------------------------------------------------------------------
// Double-layer queries
// ---------------------------------------------------------------------------

func TestTapeController_ListConversationsAndExecs(t *testing.T) {
	store := NewInMemoryTapeStore()
	tc := NewTapeController(NewTapeManager(store))

	_ = store.Append("conv:a", NewMessageEntry(map[string]any{"role": "user", "content": "hi"}))
	_ = store.Append("conv:b", NewMessageEntry(map[string]any{"role": "user", "content": "hello"}))
	_ = store.Append("conv:a:exec:1", NewExecStartEntry("e1", "agent1", nil))
	_ = store.Append("conv:a:exec:2", NewExecStartEntry("e2", "agent2", nil))
	_ = store.Append("conv:b:exec:1", NewExecStartEntry("e3", "agent3", nil))

	convs, err := tc.ListConversations()
	if err != nil {
		t.Fatal(err)
	}
	if len(convs) != 2 {
		t.Fatalf("expected 2 conversations, got %d", len(convs))
	}

	execsA, err := tc.ListExecs("conv:a")
	if err != nil {
		t.Fatal(err)
	}
	if len(execsA) != 2 {
		t.Errorf("expected 2 execs for conv:a, got %d", len(execsA))
	}

	execsB, err := tc.ListExecs("conv:b")
	if err != nil {
		t.Fatal(err)
	}
	if len(execsB) != 1 {
		t.Errorf("expected 1 exec for conv:b, got %d", len(execsB))
	}
}

// ---------------------------------------------------------------------------
// Agent ID change detection
// ---------------------------------------------------------------------------

func TestTapeController_AgentIDChangeDetection(t *testing.T) {
	store := NewInMemoryTapeStore()
	tc := NewTapeController(NewTapeManager(store))

	// First run with "planner"
	tc.RecordExecStart("conv:1", "exec-1", "planner", nil)
	tc.RecordExecInput("conv:1", "exec-1", []map[string]any{{"role": "user", "content": "task"}})
	tc.RecordExecState("conv:1", "exec-1", ExecStatePending)

	// Resume: replay shows earlier agent_id
	replay, err := tc.ReplayExec("conv:1", "exec-1")
	if err != nil {
		t.Fatal(err)
	}
	if replay.AgentID != "planner" {
		t.Fatalf("earlier AgentID = %q, want planner", replay.AgentID)
	}

	// Application layer would compare: if newAgentID != replay.AgentID { reject }
	newAgentID := "coder"
	if replay.AgentID != "" && replay.AgentID != newAgentID {
		// This is what AX does: resumption not allowed when agent ID changes
		t.Logf("resumption rejected: agent changed from %s to %s", replay.AgentID, newAgentID)
	} else {
		t.Error("expected agent ID mismatch to be detected")
	}
}

// ---------------------------------------------------------------------------
// Confirmation flow
// ---------------------------------------------------------------------------

func TestTapeController_ConfirmationFlow(t *testing.T) {
	store := NewInMemoryTapeStore()
	tc := NewTapeController(NewTapeManager(store))

	// Agent asks for confirmation
	tc.RecordExecStart("conv:1", "exec-1", "planner", nil)
	tc.RecordExecInput("conv:1", "exec-1", []map[string]any{
		{"role": "user", "content": "delete everything"},
	})
	tc.RecordExecOutput("conv:1", "exec-1", []map[string]any{
		{"role": "assistant", "content": "Are you sure you want to delete everything?"},
	})
	tc.RecordExecState("conv:1", "exec-1", ExecStatePending)

	// Resume: check state and last message
	replay, err := tc.ReplayExec("conv:1", "exec-1")
	if err != nil {
		t.Fatal(err)
	}
	if replay.State != ExecStatePending {
		t.Fatalf("State = %q, want pending", replay.State)
	}

	lastMsg := replay.Messages[len(replay.Messages)-1]
	content, _ := lastMsg["content"].(string)
	if content != "Are you sure you want to delete everything?" {
		t.Errorf("last message = %q", content)
	}

	// Simulate user approval
	tc.RecordExecInput("conv:1", "exec-1", []map[string]any{
		{"role": "user", "content": "yes, delete it"},
	})
	tc.RecordExecOutput("conv:1", "exec-1", []map[string]any{
		{"role": "assistant", "content": "done"},
	})
	tc.RecordExecState("conv:1", "exec-1", ExecStateCompleted)

	replay2, _ := tc.ReplayExec("conv:1", "exec-1")
	if replay2.State != ExecStateCompleted {
		t.Errorf("State = %q, want completed", replay2.State)
	}
}

// ---------------------------------------------------------------------------
// State transitions
// ---------------------------------------------------------------------------

func TestTapeController_ExecStateTransitions(t *testing.T) {
	store := NewInMemoryTapeStore()
	tc := NewTapeController(NewTapeManager(store))

	tc.RecordExecStart("conv:1", "exec-1", "planner", nil)
	tc.RecordExecState("conv:1", "exec-1", ExecStatePending)

	// Failed
	tc.RecordExecState("conv:1", "exec-1", ExecStateFailed)

	replay, _ := tc.ReplayExec("conv:1", "exec-1")
	if replay.State != ExecStateFailed {
		t.Errorf("State = %q, want failed", replay.State)
	}
}

// ---------------------------------------------------------------------------
// Exec entries do not leak into LLM context
// ---------------------------------------------------------------------------

func TestTapeController_ExecMessagesNotInLLMContext(t *testing.T) {
	store := NewInMemoryTapeStore()
	tc := NewTapeController(NewTapeManager(store))

	_ = store.Append("conv:1", NewMessageEntry(map[string]any{"role": "user", "content": "hello"}))
	tc.RecordExecStart("conv:1", "exec-1", "planner", nil)
	tc.RecordExecInput("conv:1", "exec-1", []map[string]any{{"role": "user", "content": "internal input"}})
	tc.RecordExecOutput("conv:1", "exec-1", []map[string]any{{"role": "assistant", "content": "internal output"}})
	tc.RecordExecState("conv:1", "exec-1", ExecStateCompleted)
	_ = store.Append("conv:1", NewMessageEntry(map[string]any{"role": "user", "content": "world"}))

	msgs, err := tc.Manager.ReadMessages("conv:1", NewNoAnchorContext())
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 LLM messages, got %d", len(msgs))
	}
	if msgs[0]["content"] != "hello" {
		t.Errorf("msg[0] = %v", msgs[0])
	}
	if msgs[1]["content"] != "world" {
		t.Errorf("msg[1] = %v", msgs[1])
	}
}

// ---------------------------------------------------------------------------
// Multi-exec interleaved replay
// ---------------------------------------------------------------------------

func TestTapeController_MultiExecInterleaved(t *testing.T) {
	store := NewInMemoryTapeStore()
	tc := NewTapeController(NewTapeManager(store))

	// Simulate planner delegating to coder
	tc.RecordExecStart("conv:1", "exec-planner", "planner", nil)
	tc.RecordExecInput("conv:1", "exec-planner", []map[string]any{{"role": "user", "content": "build a feature"}})

	// Coder sub-exec
	tc.RecordExecStart("conv:1", "exec-coder", "coder", nil)
	tc.RecordExecInput("conv:1", "exec-coder", []map[string]any{{"role": "user", "content": "write code"}})
	tc.RecordExecOutput("conv:1", "exec-coder", []map[string]any{{"role": "assistant", "content": "code done"}})
	tc.RecordExecState("conv:1", "exec-coder", ExecStateCompleted)

	// Back to planner
	tc.RecordExecOutput("conv:1", "exec-planner", []map[string]any{{"role": "assistant", "content": "feature done"}})
	tc.RecordExecState("conv:1", "exec-planner", ExecStateCompleted)

	// Replay planner exec only
	plannerReplay, _ := tc.ReplayExec("conv:1", "exec-planner")
	if len(plannerReplay.Messages) != 2 {
		t.Errorf("planner messages = %d, want 2", len(plannerReplay.Messages))
	}

	// Replay coder exec only
	coderReplay, _ := tc.ReplayExec("conv:1", "exec-coder")
	if len(coderReplay.Messages) != 2 {
		t.Errorf("coder messages = %d, want 2", len(coderReplay.Messages))
	}
}
