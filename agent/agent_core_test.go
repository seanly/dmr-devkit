package agent

import (
	"fmt"
	"testing"

	"github.com/seanly/dmr-devkit/config"
	"github.com/seanly/dmr-devkit/tape"
	"github.com/seanly/dmr-devkit/tool"
)

func TestNew_DefaultMaxSteps(t *testing.T) {
	a := New(nil, nil, nil, Config{})
	if a.config.MaxSteps != 20 {
		t.Errorf("MaxSteps = %d, want 20", a.config.MaxSteps)
	}
}

func TestNew_CustomMaxSteps(t *testing.T) {
	a := New(nil, nil, nil, Config{MaxSteps: 50})
	if a.config.MaxSteps != 50 {
		t.Errorf("MaxSteps = %d, want 50", a.config.MaxSteps)
	}
}

func TestNew_MapsInitialized(t *testing.T) {
	a := New(nil, nil, nil, Config{})
	if a.tapeStates.states == nil {
		t.Error("tapeStates.states not initialized")
	}
}

func TestGetCurrentModel_Default(t *testing.T) {
	models := []config.ModelConfig{
		{Name: "fast", Model: "gpt-4o-mini"},
		{Name: "smart", Model: "gpt-4o", Default: true},
	}
	a := New(nil, nil, nil, Config{Models: models})

	m := a.GetCurrentModel("tape1")
	if m == nil || m.Name != "smart" {
		t.Errorf("GetCurrentModel = %v, want smart", m)
	}
}

func TestGetCurrentModel_FallbackFirst(t *testing.T) {
	models := []config.ModelConfig{
		{Name: "first", Model: "m1"},
		{Name: "second", Model: "m2"},
	}
	a := New(nil, nil, nil, Config{Models: models})

	m := a.GetCurrentModel("tape1")
	if m == nil || m.Name != "first" {
		t.Errorf("GetCurrentModel fallback = %v, want first", m)
	}
}

func TestGetCurrentModel_Empty(t *testing.T) {
	a := New(nil, nil, nil, Config{})
	if m := a.GetCurrentModel("tape1"); m != nil {
		t.Errorf("GetCurrentModel(empty) = %v, want nil", m)
	}
}

func TestGetCurrentModel_Override(t *testing.T) {
	models := []config.ModelConfig{
		{Name: "fast", Model: "m1", Default: true},
		{Name: "smart", Model: "m2"},
	}
	a := New(nil, nil, nil, Config{Models: models})
	ts := a.tapeStates.getOrCreate("tape1")
	ts.mu.Lock()
	ts.modelOverride = "smart"
	ts.mu.Unlock()

	m := a.GetCurrentModel("tape1")
	if m == nil || m.Name != "smart" {
		t.Errorf("GetCurrentModel(override) = %v, want smart", m)
	}
}

func TestSwitchModel(t *testing.T) {
	models := []config.ModelConfig{
		{Name: "fast", Model: "m1", Default: true, APIKey: "k"},
		{Name: "smart", Model: "m2", APIKey: "k"},
	}
	a := New(nil, nil, nil, Config{Models: models})

	if err := a.SwitchModel("tape1", "smart"); err != nil {
		t.Fatalf("SwitchModel: %v", err)
	}
	m := a.GetCurrentModel("tape1")
	if m == nil || m.Name != "smart" {
		t.Errorf("after SwitchModel = %v, want smart", m)
	}
}

func TestSwitchModel_NotFound(t *testing.T) {
	a := New(nil, nil, nil, Config{Models: []config.ModelConfig{{Name: "x", Model: "m"}}})
	if err := a.SwitchModel("tape1", "nope"); err == nil {
		t.Error("SwitchModel(unknown) should fail")
	}
}

func TestSystemPromptBaseForTape(t *testing.T) {
	a := New(nil, nil, nil, Config{
		SystemPromptBase: "default",
		SystemPromptBases: map[string]string{
			"ci-*":   "ci prompt",
			"ci-web": "ci-web prompt",
		},
	})
	tests := []struct {
		tape string
		want string
	}{
		{"ci-build", "ci prompt"},
		{"ci-web", "ci-web prompt"}, // longer pattern wins
		{"other", "default"},
	}
	for _, tt := range tests {
		if got := a.systemPromptBaseForTape(tt.tape); got != tt.want {
			t.Errorf("systemPromptBaseForTape(%q) = %q, want %q", tt.tape, got, tt.want)
		}
	}
}

func TestModelNameForTape(t *testing.T) {
	a := New(nil, nil, nil, Config{
		TapeModels: map[string]string{
			"ci-*": "fast",
			"*":    "default",
		},
	})
	tests := []struct {
		tape string
		want string
	}{
		{"ci-build", "fast"},
		{"other", "default"},
	}
	for _, tt := range tests {
		if got := a.modelNameForTape(tt.tape); got != tt.want {
			t.Errorf("modelNameForTape(%q) = %q, want %q", tt.tape, got, tt.want)
		}
	}
}

func TestModelNameForTape_NoMatch(t *testing.T) {
	a := New(nil, nil, nil, Config{TapeModels: map[string]string{"ci-*": "fast"}})
	if got := a.modelNameForTape("other"); got != "" {
		t.Errorf("modelNameForTape(no match) = %q, want empty", got)
	}
}

func TestShouldCompactNow(t *testing.T) {
	a := New(nil, nil, nil, Config{})

	// First call: never compacted, should allow
	if !a.shouldCompactNow("tape1", 1, 0, 0, 0.8) {
		t.Error("first compact should be allowed")
	}
	a.recordCompactStep("tape1", 1)

	// Step 2: too soon (gap < 3), no token pressure
	if a.shouldCompactNow("tape1", 2, 0, 0, 0.8) {
		t.Error("step 2 should be blocked (gap < 3)")
	}

	// Step 4: allowed (gap == 3)
	if !a.shouldCompactNow("tape1", 4, 0, 0, 0.8) {
		t.Error("step 4 should be allowed (gap == 3)")
	}
	a.recordCompactStep("tape1", 4)

	// Step 1 after 4: wrap around -> reset -> allow
	if !a.shouldCompactNow("tape1", 1, 0, 0, 0.8) {
		t.Error("step wrap should allow compact")
	}

	// Pressure override: step 5 with prompt tokens above threshold should be allowed.
	a.recordCompactStep("tape1", 4)
	if !a.shouldCompactNow("tape1", 5, 8500, 10000, 0.85) {
		t.Error("step 5 should be allowed when above threshold with gap >= 1")
	}
	// But still blocked on the very next step if pressure is gone.
	if a.shouldCompactNow("tape1", 5, 0, 10000, 0.85) {
		t.Error("step 5 should still be blocked without token pressure")
	}
}

func TestCanHandoffTool(t *testing.T) {
	store := tape.NewInMemoryTapeStore()
	tm := tape.NewTapeManager(store)
	a := New(nil, tm, nil, Config{})

	// Empty tape: allow first handoff
	if !a.CanHandoffTool("tape1") {
		t.Error("empty tape should allow handoff")
	}

	// Add an anchor (simulating a previous handoff)
	_ = store.Append("tape1", tape.NewAnchorEntry("handoff/tool", nil))

	// Only a few entries after anchor: still blocked
	_ = store.Append("tape1", tape.NewMessageEntry(map[string]any{"role": "user", "content": "hi"}))
	_ = store.Append("tape1", tape.NewMessageEntry(map[string]any{"role": "assistant", "content": "hello"}))
	if a.CanHandoffTool("tape1") {
		t.Error("should block handoff with only 2 messages after anchor")
	}

	// Add more entries to reach threshold
	for i := 0; i < 4; i++ {
		_ = store.Append("tape1", tape.NewMessageEntry(map[string]any{"role": "user", "content": fmt.Sprintf("msg%d", i)}))
	}
	if !a.CanHandoffTool("tape1") {
		t.Error("should allow handoff with 6 messages after anchor")
	}
}

func TestToolDiscovery(t *testing.T) {
	a := New(nil, nil, nil, Config{})

	if a.IsToolDiscovered("tape1", "web") {
		t.Error("should not be discovered initially")
	}

	a.DiscoverTool("tape1", "web")
	if !a.IsToolDiscovered("tape1", "web") {
		t.Error("should be discovered after DiscoverTool")
	}

	// Different tape shouldn't see it
	if a.IsToolDiscovered("tape2", "web") {
		t.Error("tape2 should not see tape1's discovery")
	}

	a.ClearDiscoveredTools("tape1")
	if a.IsToolDiscovered("tape1", "web") {
		t.Error("should be cleared after ClearDiscoveredTools")
	}
}

func TestToolPersistence_PreservesNonEphemeral(t *testing.T) {
	// Register extended and MCP tools directly in the agent cache.
	a := New(nil, nil, nil, Config{})
	a.extendedTools = []*tool.Tool{
		{Spec: tool.ToolSpec{Name: "ext1", Group: tool.ToolGroupExtended}},
		{Spec: tool.ToolSpec{Name: "mcp1", Group: tool.ToolGroupMCP}},
	}
	a.extLoaded = true

	a.DiscoverTool("tape1", "ext1")
	a.DiscoverTool("tape1", "mcp1")

	// Default policy preserves extended/MCP tools.
	a.clearDiscoveredToolsWithReason("tape1", "compact")
	if !a.IsToolDiscovered("tape1", "ext1") {
		t.Error("ext1 should be preserved by default")
	}
	if !a.IsToolDiscovered("tape1", "mcp1") {
		t.Error("mcp1 should be preserved by default")
	}
}

func TestToolPersistence_ClearsEphemeral(t *testing.T) {
	a := New(nil, nil, nil, Config{})
	a.extendedTools = []*tool.Tool{
		{Spec: tool.ToolSpec{Name: "ext1", Group: tool.ToolGroupExtended}},
		{Spec: tool.ToolSpec{Name: "extTmp", Group: tool.ToolGroupExtended, Ephemeral: true}},
	}
	a.extLoaded = true

	a.DiscoverTool("tape1", "ext1")
	a.DiscoverTool("tape1", "extTmp")

	a.clearDiscoveredToolsWithReason("tape1", "compact")
	if !a.IsToolDiscovered("tape1", "ext1") {
		t.Error("non-ephemeral ext1 should be preserved")
	}
	if a.IsToolDiscovered("tape1", "extTmp") {
		t.Error("ephemeral extTmp should be cleared")
	}
}

func TestToolPersistence_ExplicitClearOnCompact(t *testing.T) {
	clear := true
	a := New(nil, nil, nil, Config{AgentPolicy: config.AgentConfig{
		ToolPersistence: &config.ToolPersistenceConfig{ClearOnCompact: &clear},
	}})
	a.extendedTools = []*tool.Tool{
		{Spec: tool.ToolSpec{Name: "ext1", Group: tool.ToolGroupExtended}},
	}
	a.extLoaded = true

	a.DiscoverTool("tape1", "ext1")
	a.clearDiscoveredToolsWithReason("tape1", "compact")
	if a.IsToolDiscovered("tape1", "ext1") {
		t.Error("ext1 should be cleared when clear_on_compact=true")
	}
}

func TestToolPersistence_KeepExtendedFalse(t *testing.T) {
	keep := false
	keepMCP := true
	a := New(nil, nil, nil, Config{AgentPolicy: config.AgentConfig{
		ToolPersistence: &config.ToolPersistenceConfig{KeepExtended: &keep, KeepMCP: &keepMCP},
	}})
	a.extendedTools = []*tool.Tool{
		{Spec: tool.ToolSpec{Name: "ext1", Group: tool.ToolGroupExtended}},
		{Spec: tool.ToolSpec{Name: "mcp1", Group: tool.ToolGroupMCP}},
	}
	a.extLoaded = true

	a.DiscoverTool("tape1", "ext1")
	a.DiscoverTool("tape1", "mcp1")
	a.clearDiscoveredToolsWithReason("tape1", "compact")
	if a.IsToolDiscovered("tape1", "ext1") {
		t.Error("ext1 should be cleared when keep_extended=false")
	}
	if !a.IsToolDiscovered("tape1", "mcp1") {
		t.Error("mcp1 should be preserved when keep_mcp=true")
	}
}

func TestGetCurrentModelName(t *testing.T) {
	models := []config.ModelConfig{
		{Name: "smart", Model: "gpt-4o", Default: true},
	}
	a := New(nil, nil, nil, Config{Models: models})

	name, model := a.GetCurrentModelName("tape1")
	if name != "smart" || model != "gpt-4o" {
		t.Errorf("GetCurrentModelName = (%q, %q), want (smart, gpt-4o)", name, model)
	}
}

func TestGetCurrentModelName_Empty(t *testing.T) {
	a := New(nil, nil, nil, Config{})
	name, model := a.GetCurrentModelName("tape1")
	if name != "" || model != "" {
		t.Errorf("GetCurrentModelName(empty) = (%q, %q), want empty", name, model)
	}
}

func TestChatClientsEviction(t *testing.T) {
	models := []config.ModelConfig{
		{Name: "m", Model: "gpt-4o", Default: true, APIKey: "k"},
	}
	a := New(nil, nil, nil, Config{Models: models})
	for i := 0; i < maxChatClients+5; i++ {
		tape := fmt.Sprintf("tape-%d", i)
		_ = a.SwitchModel(tape, "m")
	}
	a.tapeStates.mu.RLock()
	sz := len(a.tapeStates.states)
	a.tapeStates.mu.RUnlock()
	if sz > maxChatClients {
		t.Fatalf("expected tapeStates to be evicted to <= %d, got %d", maxChatClients, sz)
	}
}
