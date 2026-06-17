package handoff

import "time"

// Merge applies delta onto prev, returning a new State.
func Merge(prev *State, delta State) State {
	out := delta
	if prev == nil {
		if out.SchemaVersion == 0 {
			out.SchemaVersion = SchemaVersion
		}
		if out.UpdatedAt == "" {
			out.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		}
		return out
	}
	out.SchemaVersion = SchemaVersion
	if out.Goal == "" {
		out.Goal = prev.Goal
	}
	if out.Completed == nil {
		out.Completed = prev.Completed
	}
	if out.Pending == nil {
		out.Pending = prev.Pending
	}
	if out.Constraints == nil {
		out.Constraints = prev.Constraints
	} else if prev.Constraints != nil {
		merged := make(map[string]string, len(prev.Constraints)+len(out.Constraints))
		for k, v := range prev.Constraints {
			merged[k] = v
		}
		for k, v := range out.Constraints {
			merged[k] = v
		}
		out.Constraints = merged
	}
	if out.Artifacts == nil {
		out.Artifacts = prev.Artifacts
	}
	if out.ActiveFiles == nil {
		out.ActiveFiles = prev.ActiveFiles
	}
	if out.LastAction == "" {
		out.LastAction = prev.LastAction
	}
	if out.UpdatedAt == "" {
		out.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if out.Source == "" {
		out.Source = prev.Source
	}
	return out
}
