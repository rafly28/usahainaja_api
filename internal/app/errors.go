package app

import (
	"errors"
	"fmt"
)

var (
	ErrConflict  = errors.New("repository conflict")
	ErrNotFound  = errors.New("repository record not found")
	ErrForbidden = errors.New("repository access forbidden")
)

type Error struct {
	Code    string
	Message string
	Fields  map[string]string
	Cause   error
}

func (e *Error) Error() string {
	if e.Cause == nil {
		return e.Message
	}
	return fmt.Sprintf("%s: %v", e.Message, e.Cause)
}

func (e *Error) Unwrap() error { return e.Cause }

func validationError(fields map[string]string) *Error {
	return &Error{
		Code:    "VALIDATION_ERROR",
		Message: "Periksa kembali data yang dikirim.",
		Fields:  fields,
	}
}

func unauthorizedError() *Error {
	return &Error{Code: "UNAUTHORIZED", Message: "Email atau password tidak valid."}
}
