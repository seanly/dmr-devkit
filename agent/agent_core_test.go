package agent

import (
	"fmt"
	"testing"

	"github.com/seanly/dmr-devkit/config"
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
	if a.chatClients == nil {
		t.Error("chatClients not initialized")
	}
	if a.sessionStarted == nil {
		t.Error("sessionStarted not initialized")
	}
	if a.modelOverrides == nil {
		t.Error("modelOverrides not initialized")
	}
	if a.lastCompactStep == nil {
		t.Error("lastCompactStep not initialized")
	}
	if a.discoveredTools == nil {
		t.Error("discoveredTools not initialized")
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
	a.mu.Lock()
	a.modelOverrides["tape1"] = "smart"
	a.mu.Unlock()

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
	if !a.shouldCompactNow("tape1", 1) {
		t.Error("first compact should be allowed")
	}
	a.recordCompactStep("tape1", 1)

	// Step 2: too soon (gap < 3)
	if a.shouldCompactNow("tape1", 2) {
		t.Error("step 2 should be blocked (gap < 3)")
	}

	// Step 4: allowed (gap == 3)
	if !a.shouldCompactNow("tape1", 4) {
		t.Error("step 4 should be allowed (gap == 3)")
	}
	a.recordCompactStep("tape1", 4)

	// Step 1 after 4: wrap around -> reset -> allow
	if !a.shouldCompactNow("tape1", 1) {
		t.Error("step wrap should allow compact")
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
	a.mu.RLock()
	sz := len(a.chatClients)
	a.mu.RUnlock()
	if sz > maxChatClients {
		t.Fatalf("expected chatClients to be evicted to <= %d, got %d", maxChatClients, sz)
	}
}
