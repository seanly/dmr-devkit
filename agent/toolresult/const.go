package toolresult

// Defaults align with the Claude Code-style tool result policy.
const (
	DefaultMaxResultChars   = 50_000
	DefaultPerMessageBudget = 200_000
	DefaultPreviewRunes     = 2_000
	DefaultPersistSubdir    = ".dmr/tool-results"

	PersistedOutputTag       = "<persisted-output>"
	PersistedOutputCloseTag  = "</persisted-output>"
	ToolResultClearedMessage = "[Old tool result content cleared]"

	EmptyResultPlaceholderFormat = "(%s completed with no output)"
)
