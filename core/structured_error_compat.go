package core

// Ensure StructuredError can be matched by errors.As into *RepublicError.
// This file glues the new system to the existing RepublicError for backward compat.

// As implements the interface used by errors.As for type matching.
// It allows: var re *RepublicError; if errors.As(structuredErr, &re) { ... }
func (e *StructuredError) As(target any) bool {
	switch t := target.(type) {
	case **RepublicError:
		*t = e.ToLegacy()
		return true
	}
	return false
}

// Is implements error matching. It checks Kind equivalence when compared
// with another StructuredError or RepublicError.
func (e *StructuredError) Is(target error) bool {
	if t, ok := target.(*StructuredError); ok {
		return e.Kind == t.Kind && e.Phase == t.Phase
	}
	if t, ok := target.(*RepublicError); ok {
		return string(e.Kind) == string(t.Kind)
	}
	return false
}
