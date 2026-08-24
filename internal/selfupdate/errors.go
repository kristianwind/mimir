package selfupdate

import "fmt"

// msgError is an error whose message is a finished sentence, and whose cause
// is deliberately not part of it.
//
// These failures are read by a person on the System page — "the release ships
// no checksums.txt", "cannot write in /usr/local/bin" — where the underlying
// network or filesystem error is noise. It is wrapped rather than discarded so
// errors.Is still reaches it from the code that does care.
type msgError struct {
	msg     string
	wrapped error
}

func (e *msgError) Error() string { return e.msg }

func (e *msgError) Unwrap() error { return e.wrapped }

// errf builds one. cause may be nil.
func errf(cause error, format string, args ...any) error {
	return &msgError{msg: fmt.Sprintf(format, args...), wrapped: cause}
}
