// Package apperr defines transport-agnostic errors. Domain and application code
// return these; the gRPC layer maps Kind to a status code exactly once.
package apperr

import (
	"errors"
	"fmt"
)

// Kind classifies an error by what the caller can do about it.
type Kind int

const (
	KindInternal           Kind = iota // bug or infrastructure failure; retry may help
	KindInvalid                        // malformed input; do not retry
	KindNotFound                       // resource does not exist for this tenant
	KindConflict                       // resource already exists / idempotency mismatch
	KindFailedPrecondition             // valid input, but state forbids the operation
	KindAborted                        // concurrent modification; retry the whole operation
	KindUnauthenticated                // caller identity or tenant missing
)

// Error is a classified error with a stable machine-readable Code.
type Error struct {
	Kind    Kind
	Code    string // stable, snake_case, part of the public API contract
	Message string
	cause   error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.cause)
	}
	return e.Message
}

func (e *Error) Unwrap() error { return e.cause }

// Is matches on Code so sentinel errors compare equal after wrapping or WithCause.
func (e *Error) Is(target error) bool {
	var t *Error
	if !errors.As(target, &t) {
		return false
	}
	return t.Code == e.Code
}

// WithCause returns a copy of e carrying cause for logs; the public message is unchanged.
func (e *Error) WithCause(cause error) *Error {
	clone := *e
	clone.cause = cause
	return &clone
}

func New(kind Kind, code, message string) *Error {
	return &Error{Kind: kind, Code: code, Message: message}
}

func Invalid(code, message string) *Error { return New(KindInvalid, code, message) }

// KindOf returns the Kind of err, or KindInternal for unclassified errors.
func KindOf(err error) Kind {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return KindInternal
}
