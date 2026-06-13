package devkit

import (
	"context"
	"testing"

	"github.com/seanly/dmr-devkit/agent"
	"github.com/seanly/dmr-devkit/tape"
	"github.com/seanly/dmr-devkit/tool"
)

func TestOptions_validate(t *testing.T) {
	t.Parallel()
	var o Options
	if err := o.validate(); err == nil {
		t.Fatal("want error for empty Model")
	}
	o = Options{Model: "m"}
	if err := o.validate(); err == nil {
		t.Fatal("want error for missing APIKey")
	}
	o = Options{Model: "m", APIKey: "k"}
	if err := o.validate(); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	o = Options{
		Model: "m", TokenURL: "http://x", ClientID: "id", ClientSecret: "s",
	}
	if err := o.validate(); err == nil {
		t.Fatal("want error for OAuth without APIBase")
	}
	o = Options{
		Model: "m", APIBase: "https://api.example",
		TokenURL: "http://x", ClientID: "id", ClientSecret: "s",
	}
	if err := o.validate(); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	o = Options{Model: "m", TokenURL: "http://x"}
	if err := o.validate(); err == nil {
		t.Fatal("want error for partial OAuth")
	}
}

func TestBuild_Minimal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	kit, err := Build(ctx, Options{Model: "gpt-4o-mini", APIKey: "test-key"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = kit.Close(ctx) })

	if kit.Agent == nil || kit.Client == nil || kit.TapeManager == nil {
		t.Fatal("missing wiring")
	}
	if kit.Hooks == nil {
		t.Fatal("Hooks should be non-nil (nop or custom)")
	}
	if _, ok := kit.Store.(*tape.InMemoryTapeStore); !ok {
		t.Fatalf("Store type = %T, want *tape.InMemoryTapeStore", kit.Store)
	}
}

func TestBuild_WithTapeStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mem := tape.NewInMemoryTapeStore()
	kit, err := Build(ctx, Options{Model: "m", APIKey: "k", TapeStore: mem})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = kit.Close(ctx) })
	if kit.Store != mem {
		t.Fatal("Store should be the provided instance")
	}
}

type testHooks struct{}

func (testHooks) ComposeSystemPrompt(_ context.Context, base string) string {
	return base + "\n[testhooks]"
}

func (testHooks) CollectAllTools(context.Context, bool, bool) []*tool.Tool { return nil }

func (testHooks) AfterAgentRun(context.Context, agent.AfterAgentRunArgs) error { return nil }

func (testHooks) InterceptInput(context.Context, agent.InterceptInputArgs) (*agent.InterceptResult, error) {
	return nil, nil
}

func (testHooks) OnDiscoveredToolsCleared(context.Context, string) error { return nil }

func (testHooks) OnContextReset(context.Context, string, string) error { return nil }

func (testHooks) BeforeToolCall(context.Context, *tool.Tool, map[string]any, *tool.ToolContext) error {
	return nil
}

func (testHooks) BatchBeforeToolCall(context.Context, []tool.BatchCheckItem) map[int]error {
	return nil
}

func TestBuild_WithCustomHooks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	kit, err := Build(ctx, Options{
		Model:  "m",
		APIKey: "k",
		Hooks:  testHooks{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = kit.Close(ctx) })
}

func TestBuild_OnClose(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var closed bool
	kit, err := Build(ctx, Options{
		Model:  "m",
		APIKey: "k",
		OnClose: func(context.Context) error {
			closed = true
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := kit.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if !closed {
		t.Fatal("OnClose not invoked")
	}
}

func TestEnvOptions(t *testing.T) {
	t.Setenv("AI_MODEL", "from-env-model")
	t.Setenv("AI_API_KEY", "from-env-key")
	t.Setenv("AI_API_BASE", "https://example")
	o := EnvOptions()
	if o.Model != "from-env-model" || o.APIKey != "from-env-key" || o.APIBase != "https://example" {
		t.Fatalf("%+v", o)
	}
}
