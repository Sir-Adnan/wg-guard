package install

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/i18n"
	"github.com/Sir-Adnan/wg-guard/internal/terminal"
)

const InstallerLogPath = "/var/log/wg-guard/installer.log"

const installerLogLimit = int64(4 << 20)
const quietHeartbeatInterval = 15 * time.Second

type boundedLogWriter struct {
	io.Writer
	remaining int64
}

func (w *boundedLogWriter) Write(p []byte) (int, error) {
	if w.remaining <= 0 {
		return len(p), nil
	}
	limit := len(p)
	if int64(limit) > w.remaining {
		limit = int(w.remaining)
	}
	n, err := w.Writer.Write(p[:limit])
	w.remaining -= int64(n)
	if err != nil {
		return n, err
	}
	if n != limit {
		return n, io.ErrShortWrite
	}
	return len(p), nil
}

type quietCommandRunner interface {
	RunQuiet(context.Context, []string, time.Duration) error
}

func runQuiet(ctx context.Context, h Host, argv []string, timeout time.Duration) error {
	if runner, ok := h.(quietCommandRunner); ok {
		return runner.RunQuiet(ctx, argv, timeout)
	}
	return h.Run(ctx, argv, timeout)
}

// RunQuiet keeps verbose package/build output in a private bounded log. It is
// deliberately opt-in: ordinary host CLI commands retain their normal output.
func (realHost) RunQuiet(ctx context.Context, argv []string, timeout time.Duration) error {
	if len(argv) == 0 {
		return fmt.Errorf("installer: empty command")
	}
	if err := rotateInstallerLog(); err != nil {
		return err
	}
	log, err := os.OpenFile(InstallerLogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer log.Close()
	if err := log.Chmod(0o600); err != nil {
		return err
	}
	info, err := log.Stat()
	if err != nil {
		return err
	}
	output := &boundedLogWriter{Writer: log, remaining: installerLogLimit - info.Size()}
	name := filepath.Base(argv[0])
	// Log only the executable name. Even though installer-owned commands must
	// not carry secrets in argv, omitting arguments makes that invariant
	// defense-in-depth rather than a prerequisite for safe diagnostics.
	fmt.Fprintf(output, "\n[%s] %s\n", time.Now().UTC().Format(time.RFC3339), name)

	runCtx := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...) //nolint:gosec // explicit installer-controlled argv
	cmd.Stdout = output
	cmd.Stderr = output
	cmd.Stdin = os.Stdin
	cmd.Env = os.Environ()
	if name == "apt-get" {
		cmd.Env = append(cmd.Env, "DEBIAN_FRONTEND=noninteractive", "NEEDRESTART_MODE=a")
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Run() }()
	u := terminal.New(nil, os.Stderr, terminal.Detect(nil, os.Stderr, i18n.En))
	if err := waitQuietCommand(done, quietHeartbeatInterval, func(elapsed time.Duration) {
		u.Info(i18n.T(i18n.En, "progress.still_working", int(elapsed.Seconds())))
	}); err != nil {
		return fmt.Errorf("%s failed; details: %s: %w", name, InstallerLogPath, err)
	}
	return nil
}

func waitQuietCommand(done <-chan error, interval time.Duration, heartbeat func(time.Duration)) error {
	if interval <= 0 {
		return <-done
	}
	started := time.Now()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			return err
		case <-ticker.C:
			heartbeat(time.Since(started).Round(time.Second))
		}
	}
}

func rotateInstallerLog() error {
	if err := safeHostPath(InstallerLogPath); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(InstallerLogPath), 0o700); err != nil {
		return err
	}
	if err := os.Chmod(filepath.Dir(InstallerLogPath), 0o700); err != nil {
		return err
	}
	info, err := os.Stat(InstallerLogPath)
	if os.IsNotExist(err) || err == nil && info.Size() < installerLogLimit {
		return nil
	}
	if err != nil {
		return err
	}
	previous := InstallerLogPath + ".1"
	if err := os.Remove(previous); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(InstallerLogPath, previous)
}
