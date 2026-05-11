package workflow

import "errors"

// InterruptError signals that a node wants to pause execution and surface
// a value to an external caller (e.g. a human reviewer).
type InterruptError struct {
	Value any
}

func (e *InterruptError) Error() string { return "workflow interrupted" }

// Interrupt pauses the current node when ResumeData is absent.
// On a resumed run it returns the resume value and nil error.
func Interrupt(wctx *Context, value any) (any, error) {
	if wctx != nil && wctx.ResumeData != nil {
		data := wctx.ResumeData
		wctx.ResumeData = nil
		return data, nil
	}
	return nil, &InterruptError{Value: value}
}

// IsInterrupt reports whether err (or any error in its chain) is an *InterruptError.
func IsInterrupt(err error) bool {
	var ie *InterruptError
	return errors.As(err, &ie)
}
