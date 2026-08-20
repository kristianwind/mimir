package selfupdate

import "github.com/kristianwind/mimir/internal/i18n"

// msgError is an error that has not yet chosen a language.
//
// fmt.Errorf renders immediately, which leaves a finished sentence that no
// translation table can match — the arguments are already substituted. This
// keeps the format and its arguments apart until something knows who is
// reading, and still behaves like a normal error in the meantime: Error()
// renders the English source, and Unwrap keeps errors.Is working.
type msgError struct {
	format  string
	args    []any
	wrapped error
}

func (e *msgError) Error() string { return i18n.T(i18n.EN, e.format, e.args...) }

func (e *msgError) Unwrap() error { return e.wrapped }

// localise renders the error in lang, falling back to Error for any error that
// did not come from here.
func localise(err error, lang i18n.Lang) string {
	if err == nil {
		return ""
	}
	var m *msgError
	if as(err, &m) {
		return i18n.T(lang, m.format, m.args...)
	}
	return err.Error()
}

// errf builds a translatable error. `cause`, when not nil, is wrapped so
// errors.Is and errors.As still reach it, and is not part of the message.
func errf(cause error, format string, args ...any) error {
	return &msgError{format: format, args: args, wrapped: cause}
}

// as is errors.As specialised to *msgError, kept here so errors.go owns the
// whole mechanism.
func as(err error, target **msgError) bool {
	for err != nil {
		if m, ok := err.(*msgError); ok {
			*target = m
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
