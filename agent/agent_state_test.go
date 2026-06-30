package agent

import (
	"testing"

	"github.com/seanly/dmr-devkit/config"
	"github.com/seanly/dmr-devkit/tape"
)

func TestPersistAndRestoreTapeState(t *testing.T) {
	store := tape.NewInMemoryTapeStore()
	tm := tape.NewTapeManager(store)
	a := New(nil, tm, nil, Config{})

	a.DiscoverTool("tape1", "toolA")
	a.DiscoverTool("tape1", "toolB")
	a.recordCompactStep("tape1", 7)

	// Simulate process restart: fresh agent sharing the same store.
	a2 := New(nil, tm, nil, Config{})
	a2.restoreTapeState("tape1")

	if !a2.IsToolDiscovered("tape1", "toolA") {
		t.Error("expected toolA to be restored as discovered")
	}
	if !a2.IsToolDiscovered("tape1", "toolB") {
		t.Error("expected toolB to be restored as discovered")
	}

	ts := a2.tapeStates.get("tape1")
	if ts == nil {
		t.Fatal("expected tape state for tape1")
	}
	ts.mu.Lock()
	lastCompact := ts.lastCompactStep
	ts.mu.Unlock()
	if lastCompact != 7 {
		t.Errorf("lastCompactStep = %d, want 7", lastCompact)
	}
}

func TestAgentStateEntryWrittenOnDiscovery(t *testing.T) {
	store := tape.NewInMemoryTapeStore()
	tm := tape.NewTapeManager(store)
	a := New(nil, tm, nil, Config{})

	a.DiscoverTool("tape1", "toolA")

	entries, err := store.FetchAll("tape1", nil)
	if err != nil {
		t.Fatalf("FetchAll: %v", err)
	}

	var found bool
	for _, e := range entries {
		if e.Kind != "agent_state" {
			continue
		}
		dt, ok := e.Payload["discovered_tools"].([]string)
		if !ok {
			continue
		}
		for _, name := range dt {
			if name == "toolA" {
				found = true
				break
			}
		}
	}
	if !found {
		t.Error("expected agent_state entry with discovered tool toolA")
	}
}

func TestAgentStateEntriesWritten(t *testing.T) {
	store := tape.NewInMemoryTapeStore()
	tm := tape.NewTapeManager(store)
	a := New(nil, tm, nil, Config{})

	a.DiscoverTool("tape1", "toolA")
	a.DiscoverTool("tape1", "toolB")
	a.recordCompactStep("tape1", 3)
	a.ClearDiscoveredTools("tape1")

	entries, err := store.FetchAll("tape1", nil)
	if err != nil {
		t.Fatalf("FetchAll: %v", err)
	}

	var agentStateCount int
	var lastPayload map[string]any
	for _, e := range entries {
		if e.Kind == "agent_state" {
			agentStateCount++
			lastPayload = e.Payload
		}
	}
	if agentStateCount < 2 {
		t.Fatalf("expected at least 2 agent_state entries, got %d", agentStateCount)
	}
	if lastPayload == nil {
		t.Fatal("expected last agent_state payload")
	}

	dt, ok := lastPayload["discovered_tools"].([]string)
	if !ok {
		t.Fatalf("discovered_tools type = %T, want []string", lastPayload["discovered_tools"])
	}
	if len(dt) != 0 {
		t.Errorf("after ClearDiscoveredTools, discovered_tools = %v, want empty", dt)
	}
}

func TestRestoreTapeState_ModelOverride(t *testing.T) {
	models := []config.ModelConfig{
		{Name: "fast", Model: "gpt-4o-mini", Default: true, APIKey: "k"},
		{Name: "smart", Model: "gpt-4o", APIKey: "k"},
	}
	store := tape.NewInMemoryTapeStore()
	tm := tape.NewTapeManager(store)
	a := New(nil, tm, nil, Config{Models: models})

	if err := a.SwitchModel("tape1", "smart"); err != nil {
		t.Fatalf("SwitchModel: %v", err)
	}
	a.persistTapeState("tape1")

	a2 := New(nil, tm, nil, Config{Models: models})
	a2.restoreTapeState("tape1")

	m := a2.GetCurrentModel("tape1")
	if m == nil || m.Name != "smart" {
		t.Errorf("restored model = %v, want smart", m)
	}
}

func TestPersistTapeState_SkipsChildTape(t *testing.T) {
	store := tape.NewInMemoryTapeStore()
	tm := tape.NewTapeManager(store)
	a := New(nil, tm, nil, Config{})

	childTape := "parent:subagent:abc123"
	a.DiscoverTool(childTape, "toolA")
	a.recordCompactStep(childTape, 2)

	entries, err := store.FetchAll(childTape, nil)
	if err != nil {
		t.Fatalf("FetchAll: %v", err)
	}
	for _, e := range entries {
		if e.Kind == "agent_state" {
			t.Error("agent_state entry should not be written for transient child tape")
		}
	}
}
