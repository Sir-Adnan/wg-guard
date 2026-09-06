package install

import (
	"github.com/Sir-Adnan/wg-guard/internal/i18n"
	"github.com/Sir-Adnan/wg-guard/internal/terminal"
	"io"
)

type progressOutput struct {
	io.Writer
	ui *terminal.UI
}

func progressUI(out io.Writer) *terminal.UI {
	if p, ok := out.(*progressOutput); ok {
		return p.ui
	}
	return terminal.New(nil, out, terminal.Detect(nil, out, i18n.En))
}
func progress(out io.Writer, key string, args ...any) {
	u := progressUI(out)
	u.Text(u.T("progress."+key, args...))
}

// TerminalError keeps a catalog key and cause for the bilingual terminal UI.
// Error retains the existing CLI's English default; Localized renders fa/en.
type TerminalError struct {
	Key   string
	Args  []any
	Cause error
}

func (e *TerminalError) Error() string                       { return e.Localized(i18n.En) }
func (e *TerminalError) Localized(locale i18n.Locale) string { return i18n.T(locale, e.Key, e.Args...) }
func (e *TerminalError) Unwrap() error                       { return e.Cause }
func terminalError(key string, args ...any) error {
	e := &TerminalError{Key: key, Args: args}
	for _, arg := range args {
		if cause, ok := arg.(error); ok {
			e.Cause = cause
		}
	}
	return e
}
