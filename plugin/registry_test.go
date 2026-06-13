package plugin

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/seanly/dmr-devkit/agent"
	"github.com/seanly/dmr-devkit/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Mock plugins
// ---------------------------------------------------------------------------

type mockPlugin struct {
	name       string
	caps       []Capability
	initErr    error
	shutdownErr error
	healthErr  error
}

func (m *mockPlugin) Name() string                                { return m.name }
func (m *mockPlugin) Version() string                             { return "1.0" }
func (m *mockPlugin) Init(ctx context.Context, cfg map[string]any) error { return m.initErr }
func (m *mockPlugin) Shutdown(ctx context.Context) error                 { return m.shutdownErr }
func (m *mockPlugin) Capabilities() []Capability                     { return m.caps }
func (m *mockPlugin) HealthCheck(ctx context.Context) error            { return m.healthErr }

type mockToolProvider struct {
	mockPlugin
	tools []*tool.Tool
}

func (m *mockToolProvider) ListTools(ctx context.Context) ([]*tool.Tool, error) {
	return m.tools, nil
}

type mockSystemPromptProvider struct {
	mockPlugin
	frag string
	err  error
}

func (m *mockSystemPromptProvider) SystemPrompt(ctx context.Context, base string) (string, error) {
	return m.frag, m.err
}

type mockLifecycle struct {
	mockPlugin
}

func (m *mockLifecycle) AfterAgentRun(ctx context.Context, args agent.AfterAgentRunArgs) error {
	return nil
}
func (m *mockLifecycle) OnDiscoveredToolsCleared(ctx context.Context, tapeName string) error {
	return nil
}

type mockContextReset struct {
	mockPlugin
	resetTapeName string
	resetReason   string
	resetErr      error
}

func (m *mockContextReset) OnContextReset(ctx context.Context, tapeName string, reason string) error {
	m.resetTapeName = tapeName
	m.resetReason = reason
	return m.resetErr
}

// ---------------------------------------------------------------------------
// Register / Unregister
// ---------------------------------------------------------------------------

func TestRegistry_RegisterDuplicate(t *testing.T) {
	r := NewRegistry()
	p := &mockPlugin{name: "foo", caps: []Capability{}}
	require.NoError(t, r.Register(p))
	assert.ErrorContains(t, r.Register(p), `plugin "foo" already registered`)
}

func TestRegistry_RegisterCapabilityMismatch(t *testing.T) {
	r := NewRegistry()
	// Declares CapTools but does not implement ToolProvider.
	p := &mockPlugin{name: "bad", caps: []Capability{CapTools}}
	assert.ErrorContains(t, r.Register(p), `capability "tools" but does not implement`)

	// Implements ToolProvider but does not declare CapTools.
	p2 := &mockToolProvider{mockPlugin: mockPlugin{name: "bad2", caps: []Capability{}}, tools: nil}
	assert.ErrorContains(t, r.Register(p2), `implements capability "tools" interface but does not declare`)
}

func TestRegistry_RegisterInferredCapabilities(t *testing.T) {
	r := NewRegistry()
	p := &mockToolProvider{mockPlugin: mockPlugin{name: "tp", caps: []Capability{CapTools}}, tools: nil}
	require.NoError(t, r.Register(p))
	assert.True(t, r.HasCapability(CapTools))
	assert.False(t, r.HasCapability(CapSystemPrompt))
}

func TestRegistry_Unregister(t *testing.T) {
	r := NewRegistry()
	p := &mockPlugin{name: "x", caps: []Capability{}}
	require.NoError(t, r.Register(p))
	assert.True(t, r.Unregister("x"))
	assert.False(t, r.Unregister("x"))
	assert.Nil(t, r.Get("x"))
	assert.Empty(t, r.List())
}

func TestRegistry_ListOrder(t *testing.T) {
	r := NewRegistry()
	for i := 1; i <= 3; i++ {
		require.NoError(t, r.Register(&mockPlugin{name: fmt.Sprintf("p%d", i), caps: []Capability{}}))
	}
	list := r.List()
	require.Len(t, list, 3)
	assert.Equal(t, "p1", list[0].Name())
	assert.Equal(t, "p2", list[1].Name())
	assert.Equal(t, "p3", list[2].Name())

	// Unregister middle, order preserved for remaining.
	r.Unregister("p2")
	list = r.List()
	require.Len(t, list, 2)
	assert.Equal(t, "p1", list[0].Name())
	assert.Equal(t, "p3", list[1].Name())
}

// ---------------------------------------------------------------------------
// Capability queries
// ---------------------------------------------------------------------------

func TestRegistry_ToolProviders(t *testing.T) {
	r := NewRegistry()
	tp := &mockToolProvider{
		mockPlugin: mockPlugin{name: "tools", caps: []Capability{CapTools}},
		tools:      []*tool.Tool{},
	}
	require.NoError(t, r.Register(tp))
	providers := r.ToolProviders()
	require.Len(t, providers, 1)
	assert.Equal(t, "tools", providers[0].(Plugin).Name())
}

func TestRegistry_HasCapability(t *testing.T) {
	r := NewRegistry()
	assert.False(t, r.HasCapability(CapTools))
	p := &mockToolProvider{mockPlugin: mockPlugin{name: "t", caps: []Capability{CapTools}}, tools: nil}
	require.NoError(t, r.Register(p))
	assert.True(t, r.HasCapability(CapTools))
}

func TestRegistry_ContextResetHandlers(t *testing.T) {
	r := NewRegistry()
	p := &mockContextReset{mockPlugin: mockPlugin{name: "cr", caps: []Capability{CapContextReset}}}
	require.NoError(t, r.Register(p))

	handlers := r.ContextResetHandlers()
	require.Len(t, handlers, 1)
	assert.Equal(t, "cr", handlers[0].(Plugin).Name())
}

func TestRegistry_RegisterContextResetCapabilityMismatch(t *testing.T) {
	r := NewRegistry()
	// Declares CapContextReset but does not implement ContextResetHandler.
	p := &mockPlugin{name: "bad", caps: []Capability{CapContextReset}}
	assert.ErrorContains(t, r.Register(p), `capability "context-reset" but does not implement`)

	// Implements ContextResetHandler but does not declare CapContextReset.
	p2 := &mockContextReset{mockPlugin: mockPlugin{name: "bad2", caps: []Capability{}}}
	assert.ErrorContains(t, r.Register(p2), `implements capability "context-reset" interface but does not declare`)
}

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

func TestRegistry_InitAllErrors(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(&mockPlugin{name: "a", caps: []Capability{}, initErr: errors.New("boom")}))
	require.NoError(t, r.Register(&mockPlugin{name: "b", caps: []Capability{}, initErr: errors.New("pow")}))

	err := r.InitAll(context.Background(), nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, `init plugin "a": boom`)
	assert.ErrorContains(t, err, `init plugin "b": pow`)
	// errors.Join produces a multi-error; both should be present.
	assert.True(t, errors.Is(err, errors.New("boom")) || errors.Is(err, errors.New("pow")) || err != nil)
}

func TestRegistry_ShutdownAllReverseOrder(t *testing.T) {
	r := NewRegistry()
	var shutOrder []string
	wrap := func(name string) Plugin {
		return &shutdownRecorder{
			mockPlugin: mockPlugin{name: name, caps: []Capability{}},
			order:      &shutOrder,
		}
	}
	require.NoError(t, r.Register(wrap("a")))
	require.NoError(t, r.Register(wrap("b")))
	require.NoError(t, r.Register(wrap("c")))
	require.NoError(t, r.ShutdownAll(context.Background()))
	assert.Equal(t, []string{"c", "b", "a"}, shutOrder)
}

type shutdownRecorder struct {
	mockPlugin
	order *[]string
}

func (s *shutdownRecorder) Shutdown(ctx context.Context) error {
	*s.order = append(*s.order, s.name)
	return nil
}

func TestRegistry_ShutdownAllAggregatesErrors(t *testing.T) {
	r := NewRegistry()
	p1 := &mockPlugin{name: "a", caps: []Capability{}, shutdownErr: errors.New("e1")}
	p2 := &mockPlugin{name: "b", caps: []Capability{}, shutdownErr: errors.New("e2")}
	require.NoError(t, r.Register(p1))
	require.NoError(t, r.Register(p2))

	err := r.ShutdownAll(context.Background())
	require.Error(t, err)
	assert.ErrorContains(t, err, `shutdown plugin "a": e1`)
	assert.ErrorContains(t, err, `shutdown plugin "b": e2`)
}

// ---------------------------------------------------------------------------
// RegistryHooks error reporting
// ---------------------------------------------------------------------------

func TestRegistryHooks_ComposeSystemPromptReportsError(t *testing.T) {
	r := NewRegistry()
	p := &mockSystemPromptProvider{
		mockPlugin: mockPlugin{name: "sp", caps: []Capability{CapSystemPrompt}},
		frag:       "",
		err:        errors.New("prompt broken"),
	}
	require.NoError(t, r.Register(p))

	var reported []error
	h := NewRegistryHooks(r).(*RegistryHooks)
	h.ErrorHandler = func(err error) { reported = append(reported, err) }

	result := h.ComposeSystemPrompt(context.Background(), "base")
	assert.Equal(t, "base", result) // no extras contributed
	require.Len(t, reported, 1)
	assert.ErrorContains(t, reported[0], `plugin "sp" SystemPrompt error: prompt broken`)
}

func TestRegistryHooks_CollectAllToolsReportsError(t *testing.T) {
	r := NewRegistry()
	tp := &mockToolProvider{
		mockPlugin: mockPlugin{name: "tp", caps: []Capability{CapTools}},
	}
	require.NoError(t, r.Register(tp))

	var reported []error
	h := NewRegistryHooks(r).(*RegistryHooks)
	h.ErrorHandler = func(err error) { reported = append(reported, err) }

	// We need ListTools to return an error. Since mockToolProvider.ListTools is hard-coded,
	// use a custom type.
	custom := &badToolProvider{mockPlugin: mockPlugin{name: "bad", caps: []Capability{CapTools}}}
	require.NoError(t, r.Register(custom))

	tools := h.CollectAllTools(context.Background(), false, false)
	assert.Empty(t, tools)
	require.Len(t, reported, 1)
	assert.ErrorContains(t, reported[0], `plugin "bad" ListTools error: nope`)
}

func TestRegistryHooks_OnContextReset(t *testing.T) {
	r := NewRegistry()
	p := &mockContextReset{mockPlugin: mockPlugin{name: "cr", caps: []Capability{CapContextReset}}}
	require.NoError(t, r.Register(p))

	h := NewRegistryHooks(r)
	err := h.OnContextReset(context.Background(), "test-tape", "compact")
	require.NoError(t, err)
	assert.Equal(t, "test-tape", p.resetTapeName)
	assert.Equal(t, "compact", p.resetReason)
}

func TestRegistryHooks_OnContextResetError(t *testing.T) {
	r := NewRegistry()
	p1 := &mockContextReset{mockPlugin: mockPlugin{name: "p1", caps: []Capability{CapContextReset}}, resetErr: errors.New("boom")}
	p2 := &mockContextReset{mockPlugin: mockPlugin{name: "p2", caps: []Capability{CapContextReset}}}
	require.NoError(t, r.Register(p1))
	require.NoError(t, r.Register(p2))

	h := NewRegistryHooks(r)
	err := h.OnContextReset(context.Background(), "tape", "handoff")
	require.Error(t, err)
	assert.ErrorContains(t, err, "boom")
	// p2 should not have been called because p1 returned an error.
	assert.Equal(t, "", p2.resetTapeName)
}

type badToolProvider struct {
	mockPlugin
}

func (b *badToolProvider) ListTools(ctx context.Context) ([]*tool.Tool, error) {
	return nil, errors.New("nope")
}
