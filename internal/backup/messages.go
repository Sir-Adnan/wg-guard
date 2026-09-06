package backup

import (
	"encoding/json"
	"errors"
	"github.com/Sir-Adnan/wg-guard/internal/i18n"
)

// Message retains a safe catalog identity across service/presentation boundaries.
// Causes remain available to errors.Is, but are never interpolated into public text.
type Message struct {
	Key   string
	Args  []any
	Cause error
}

func (m Message) Error() string  { return m.Localized(i18n.En) }
func (m Message) String() string { return m.Error() }

// Structured warning logs must not serialize internal causes (HTTP URLs may
// contain credentials). Emit only the same safe public message as text logs.
func (m Message) MarshalJSON() ([]byte, error) { return json.Marshal(m.Error()) }
func (m Message) Unwrap() error                { return m.Cause }
func (m Message) Localized(locale i18n.Locale) string {
	args := append([]any(nil), m.Args...)
	for i, arg := range args {
		if e, ok := arg.(error); ok {
			args[i] = ErrorText(e, locale)
		}
	}
	return i18n.T(locale, "backup.safety."+m.Key, args...)
}
func warning(key string, args ...any) Message { return Message{Key: key, Args: args} }
func safetyError(key string, cause error, args ...any) error {
	return Message{Key: key, Cause: cause, Args: args}
}
func ErrorText(err error, locale i18n.Locale) string {
	if err == nil {
		return ""
	}
	var translated interface{ Localized(i18n.Locale) string }
	if errors.As(err, &translated) {
		return translated.Localized(locale)
	}
	return err.Error()
}

type localizedError struct {
	error
	locale i18n.Locale
}

func (e localizedError) Error() string { return ErrorText(e.error, e.locale) }
func (e localizedError) Unwrap() error { return e.error }
func InLocale(err error, locale i18n.Locale) error {
	if err == nil {
		return nil
	}
	return localizedError{err, locale}
}
func WarningTexts(messages []Message, locale i18n.Locale) []string {
	out := make([]string, len(messages))
	for i, m := range messages {
		out[i] = m.Localized(locale)
	}
	return out
}
