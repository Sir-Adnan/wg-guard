package install

import (
	"io"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/i18n"
	"github.com/Sir-Adnan/wg-guard/internal/terminal"
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
	message := u.T("progress."+key, args...)
	switch key {
	case "healthy", "started", "port_free", "dns", "certificate", "nginx_challenge", "nginx", "core_ready", "update_complete":
		u.Success(message)
	case "persistence", "dns_pending", "runtime_legacy_builder":
		u.Warning(message)
	default:
		u.Info(message)
	}
}

// trackedTask covers host work that does not stream its own command progress,
// such as a pre-update backup or a bounded health wait.
func trackedTask(out io.Writer, label string, run func() error) error {
	task := progressUI(out).BeginTask(label)
	stop := make(chan struct{})
	done := make(chan struct{})
	started := time.Now()
	go func() {
		defer close(done)
		ticker := time.NewTicker(quietHeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				task.Tick(time.Since(started))
			}
		}
	}()
	err := run()
	close(stop)
	<-done
	task.Done(err)
	return err
}

// TerminalError keeps a catalog key and cause for the terminal UI.
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
