package core

import (
	"errors"
	"fmt"
)

// ErrorKind classifies the kind of error that occurred.
type ErrorKind string

const (
	ErrInvalidInput ErrorKind = "invalid_input"
	ErrConfig       ErrorKind = "config"
	ErrProvider     ErrorKind = "provider"
	ErrTool         ErrorKind = "tool"
	ErrTemporary    ErrorKind = "temporary"
	ErrNotFound     ErrorKind = "not_found"
	ErrDenied       ErrorKind = "denied"
	ErrUnknown      ErrorKind = "unknown"
)

// RepublicError is the primary error type for the republic library.
type RepublicError struct {
	Kind    ErrorKind
	Message string
	Cause   error
}

func (e *RepublicError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Kind, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}

func (e *RepublicError) Unwrap() error {
	return e.Cause
}

// NewError creates a new RepublicError.
func NewError(kind ErrorKind, message string, cause error) *RepublicError {
	return &RepublicError{Kind: kind, Message: message, Cause: cause}
}

// IsErrorKind checks whether err is a *RepublicError with the given kind.
func IsErrorKind(err error, kind ErrorKind) bool {
	var re *RepublicError
	if errors.As(err, &re) {
		return re.Kind == kind
	}
	return false
}
