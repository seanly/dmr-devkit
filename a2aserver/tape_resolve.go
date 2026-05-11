package a2aserver

import (
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

// TapeMode selects how the A2A executor chooses the tape name passed to [Runner.Run].
type TapeMode string

const (
	// TapeModeAuto derives one tape per A2A task from TapePrefix and [a2asrv.ExecutorContext.TaskID].
	TapeModeAuto TapeMode = "auto"
	// TapeModeFixed uses [Options.TapeName] for every request (legacy; concurrent clients share one tape).
	TapeModeFixed TapeMode = "fixed"
)

// SanitizeTapeSegment reduces a string to a single safe path segment for tape file names
// (alphanumeric, underscore, hyphen, dot). Other runes become underscores.
func SanitizeTapeSegment(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	out = strings.Trim(out, "._-")
	if out == "" || out == "." || out == ".." {
		return "tape"
	}
	return out
}

// AutoTapeName returns a flat tape name prefix_taskId safe for [tape.FileTapeStore] (no extra path segments).
func AutoTapeName(prefix string, taskID a2a.TaskID) string {
	p := SanitizeTapeSegment(prefix)
	if p == "" {
		p = "a2a"
	}
	id := SanitizeTapeSegment(string(taskID))
	if id == "" || id == "tape" {
		id = "task"
	}
	return p + "_" + id
}

func (o *Options) resolveTape(execCtx *a2asrv.ExecutorContext) string {
	if o.TapeMode == TapeModeFixed {
		return o.TapeName
	}
	return AutoTapeName(o.TapePrefix, execCtx.TaskID)
}
