package toolresult

// MicrocompactPolicy clears old tool messages on the wire (not on tape) before LLM calls.
type MicrocompactPolicy struct {
	Enabled          bool
	KeepRecent       int
	CompactableTools map[string]struct{} // empty = no tool is compactable
	GapMinutes       float64             // 0 = disable time-based trigger; >0 requires last message time
	MaxAgeTurns      int                 // 0 = disable age-based trigger; >0 clears tool results older than N assistant turns
	SizeThreshold    int                 // 0 = disable size-based trigger; >0 immediately externalizes single results larger than threshold runes
}

// Policy configures tool result externalization and compaction.
type Policy struct {
	Workspace       string
	DefaultMaxChars int // per-result externalize threshold clamp (0 = DefaultMaxResultChars)
	PerMessageBudget int // aggregate rune budget per parallel tool batch (0 = DefaultPerMessageBudget)
	PreviewRunes    int
	PersistSubdir   string
	Microcompact    MicrocompactPolicy
	// SkipTools never externalizes output for these names (e.g. fsRead to avoid read-loop).
	SkipTools map[string]struct{}
}

func (p *Policy) effectiveMaxChars() int {
	if p.DefaultMaxChars > 0 {
		return p.DefaultMaxChars
	}
	return DefaultMaxResultChars
}

func (p *Policy) effectiveBudget() int {
	if p.PerMessageBudget > 0 {
		return p.PerMessageBudget
	}
	return DefaultPerMessageBudget
}

func (p *Policy) effectivePreviewRunes() int {
	if p.PreviewRunes > 0 {
		return p.PreviewRunes
	}
	return DefaultPreviewRunes
}

func (p *Policy) persistSubdir() string {
	if p.PersistSubdir != "" {
		return p.PersistSubdir
	}
	return DefaultPersistSubdir
}

func (p *Policy) skips(toolName string) bool {
	if p.SkipTools == nil {
		return false
	}
	_, ok := p.SkipTools[toolName]
	return ok
}

// DefaultMicrocompactTools is a sensible default set for CLI-style agents (names match common DMR tools).
func DefaultMicrocompactTools() map[string]struct{} {
	return map[string]struct{}{
		"shell": {}, "shellOutput": {}, "powershell": {}, "powershellOutput": {},
		"fsRead": {}, "fsGrep": {}, "fsGlob": {}, "webFetch": {},
	}
}
