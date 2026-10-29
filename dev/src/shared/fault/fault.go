// Package fault defines public-safe application errors. Internal causes never
// become a response body or a durable job's last_safe_error.
package fault

import "errors"

type Code string

const (
	Invalid         Code = "INVALID_REQUEST"
	Unauthenticated Code = "UNAUTHENTICATED"
	Forbidden       Code = "FORBIDDEN"
	NotFound        Code = "NOT_FOUND"
	Conflict        Code = "CONFLICT"
	Unavailable     Code = "UNAVAILABLE"
	CommitUnknown   Code = "COMMIT_UNKNOWN"
)

type Error struct {
	Code      Code
	Message   string
	Retryable bool
	cause     error
}

func (e *Error) Error() string             { return string(e.Code) + ": " + e.Message }
func (e *Error) Unwrap() error             { return e.cause }
func New(code Code, message string) *Error { return &Error{Code: code, Message: message} }
func Wrap(code Code, message string, cause error) *Error {
	return &Error{Code: code, Message: message, cause: cause}
}
func Public(err error) *Error {
	var safe *Error
	if errors.As(err, &safe) {
		return safe
	}
	return &Error{Code: Unavailable, Message: "Service temporarily unavailable", Retryable: true, cause: err}
}
