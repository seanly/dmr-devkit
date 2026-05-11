package core

import (
	"errors"
	"fmt"
	"testing"
)

func TestErrorKindValues(t *testing.T) {
	cases := map[ErrorKind]string{
		ErrInvalidInput: "invalid_input",
		ErrConfig:       "config",
		ErrProvider:     "provider",
		ErrTool:         "tool",
		ErrTemporary:    "temporary",
		ErrNotFound:     "not_found",
		ErrUnknown:      "unknown",
	}
	for kind, want := range cases {
		if string(kind) != want {
			t.Errorf("ErrorKind %q != %q", kind, want)
		}
	}
}

func TestRepublicErrorMessage(t *testing.T) {
	err := &RepublicError{Kind: ErrProvider, Message: "server error"}
	want := "provider: server error"
	if err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
}

func TestRepublicErrorMessageWithCause(t *testing.T) {
	cause := fmt.Errorf("connection refused")
	err := &RepublicError{Kind: ErrTemporary, Message: "retry failed", Cause: cause}
	want := "temporary: retry failed: connection refused"
	if err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
}

func TestRepublicErrorUnwrap(t *testing.T) {
	cause := fmt.Errorf("root cause")
	err := &RepublicError{Kind: ErrProvider, Message: "wrap", Cause: cause}
	if err.Unwrap() != cause {
		t.Error("Unwrap did not return cause")
	}
}

func TestRepublicErrorNilCause(t *testing.T) {
	err := &RepublicError{Kind: ErrConfig, Message: "no cause"}
	if err.Unwrap() != nil {
		t.Error("Unwrap should return nil when Cause is nil")
	}
}

func TestNewError(t *testing.T) {
	cause := fmt.Errorf("underlying")
	err := NewError(ErrTool, "tool failed", cause)
	if err.Kind != ErrTool {
		t.Errorf("Kind = %q, want %q", err.Kind, ErrTool)
	}
	if err.Message != "tool failed" {
		t.Errorf("Message = %q", err.Message)
	}
	if err.Cause != cause {
		t.Error("Cause mismatch")
	}
}

func TestIsErrorKind(t *testing.T) {
	err := NewError(ErrTemporary, "timeout", nil)
	if !IsErrorKind(err, ErrTemporary) {
		t.Error("expected IsErrorKind to return true")
	}
}

func TestIsErrorKindMismatch(t *testing.T) {
	err := NewError(ErrTemporary, "timeout", nil)
	if IsErrorKind(err, ErrProvider) {
		t.Error("expected IsErrorKind to return false for different kind")
	}
}

func TestIsErrorKindNonRepublicError(t *testing.T) {
	err := fmt.Errorf("plain error")
	if IsErrorKind(err, ErrUnknown) {
		t.Error("expected IsErrorKind to return false for non-RepublicError")
	}
}

func TestIsErrorKindWrappedError(t *testing.T) {
	inner := NewError(ErrNotFound, "missing", nil)
	wrapped := fmt.Errorf("outer: %w", inner)
	if !IsErrorKind(wrapped, ErrNotFound) {
		t.Error("expected IsErrorKind to find wrapped RepublicError")
	}
}

func TestRepublicErrorImplementsErrorInterface(t *testing.T) {
	var err error = NewError(ErrConfig, "bad config", nil)
	if err == nil {
		t.Error("should implement error interface")
	}
}

func TestRepublicErrorWorksWithErrorsIs(t *testing.T) {
	cause := fmt.Errorf("sentinel")
	err := NewError(ErrProvider, "wrapped", cause)
	if !errors.Is(err, cause) {
		t.Error("errors.Is should find cause through Unwrap")
	}
}
