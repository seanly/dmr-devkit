package workflow

// Context carries execution state through workflow nodes.
// It is intentionally lightweight and free of DMR-specific imports so that
// callers (including pkg/devkit) can bridge it to Tape or other stores.
type Context struct {
	// State is a mutable key-value bag shared across all nodes in a run.
	// Nodes may read and write State to pass data without mutating inputs.
	State map[string]any

	// Metadata is read-only configuration injected by the caller.
	// Typical keys: "tape_name", "workspace", "agent_runner", etc.
	Metadata map[string]any

	// Step is the current execution counter (0-based before first step).
	Step int

	// StepLog records every executed step for resume and observability.
	StepLog []LogEntry

	// ResumeData is injected by the caller before resuming an interrupted
	// workflow. Nodes should consume it via workflow.Interrupt.
	ResumeData any
}

// NewContext creates a fresh workflow context.
func NewContext() *Context {
	return &Context{
		State:    make(map[string]any),
		Metadata: make(map[string]any),
		Step:     0,
		StepLog:  make([]LogEntry, 0),
	}
}

// WithMetadata returns a shallow clone with additional metadata merged in.
func (c *Context) WithMetadata(kv map[string]any) *Context {
	if c == nil {
		c = NewContext()
	}
	clone := &Context{
		State:      make(map[string]any, len(c.State)),
		Metadata:   make(map[string]any, len(c.Metadata)+len(kv)),
		Step:       c.Step,
		StepLog:    append([]LogEntry(nil), c.StepLog...),
		ResumeData: c.ResumeData,
	}
	for k, v := range c.State {
		clone.State[k] = v
	}
	for k, v := range c.Metadata {
		clone.Metadata[k] = v
	}
	for k, v := range kv {
		clone.Metadata[k] = v
	}
	return clone
}

// GetState returns a value from State.
func (c *Context) GetState(key string) (any, bool) {
	if c == nil || c.State == nil {
		return nil, false
	}
	v, ok := c.State[key]
	return v, ok
}

// SetState writes a value into State.
func (c *Context) SetState(key string, value any) {
	if c == nil {
		return
	}
	if c.State == nil {
		c.State = make(map[string]any)
	}
	c.State[key] = value
}
