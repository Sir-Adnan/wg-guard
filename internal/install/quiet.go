package install

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/i18n"
	"github.com/Sir-Adnan/wg-guard/internal/logsafe"
	"github.com/Sir-Adnan/wg-guard/internal/terminal"
)

const (
	InstallerLogDir  = "/var/log/wg-guard"
	InstallerLogPath = InstallerLogDir + "/installer.log"
)

const installerLogLimit = int64(4 << 20)
const quietHeartbeatInterval = 15 * time.Second

// Source acquisition happens before any deployment operation. Its failures must
// still appear in the private installer log; no stdout, argv or source is logged.
func RecordAcquisitionFailure(cause error) error {
	if cause == nil {
		return nil
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
	return writeAcquisitionFailure(output, cause, time.Now().UTC())
}

func writeAcquisitionFailure(output io.Writer, cause error, at time.Time) error {
	message := logsafe.RedactText(cause.Error())
	message = strings.Join(strings.Fields(message), " ")
	if len(message) > 2048 {
		message = "…" + strings.ToValidUTF8(message[len(message)-2048:], "�")
	}
	_, err := fmt.Fprintf(output, "\n[%s] acquisition failed: %s\n", at.Format(time.RFC3339), message)
	return err
}

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
	argv = withAptLockWait(argv)
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
	task := u.BeginTask(quietCommandLabel(argv))
	err = waitQuietCommand(done, quietHeartbeatInterval, task.Tick)
	task.Done(err)
	if err != nil {
		return fmt.Errorf("%s failed; details: %s: %w", name, InstallerLogPath, err)
	}
	return nil
}

// Only fixed, non-secret command categories are shown. Raw argv can include
// paths and configuration values and must never become terminal progress copy.
func quietCommandLabel(argv []string) string {
	if len(argv) == 0 {
		return "Running installation task"
	}
	arg := func(n int) string {
		if len(argv) > n {
			return argv[n]
		}
		return ""
	}
	switch filepath.Base(argv[0]) {
	case "apt-get":
		for _, argument := range argv[1:] {
			if argument == "update" {
				return "Refreshing Ubuntu package index"
			}
		}
		return "Installing required Ubuntu packages"
	case "docker":
		switch arg(1) {
		case "build":
			return "Building verified Docker runtime"
		case "pull":
			return "Downloading Docker runtime"
		case "compose":
			for _, action := range argv[2:] {
				switch action {
				case "version":
					return "Checking Docker Compose"
				case "up":
					return "Starting Docker service"
				case "down":
					return "Stopping Docker service"
				}
			}
			return "Preparing Docker service"
		case "info":
			return "Checking Docker daemon"
		}
	case "git":
		return "Fetching reviewed AmneziaWG source"
	case "make":
		if argv[len(argv)-1] == "clean" {
			return "Cleaning reviewed AmneziaWG build"
		}
		return "Building reviewed AmneziaWG tools"
	case "dkms":
		switch arg(1) {
		case "install":
			return "Building AmneziaWG kernel module"
		case "remove":
			return "Removing incomplete kernel module"
		default:
			return "Registering AmneziaWG kernel module"
		}
	case "depmod":
		return "Refreshing kernel module index"
	case "nginx":
		return "Checking Nginx configuration"
	case "systemd-tmpfiles":
		return "Applying log retention policy"
	case "systemctl":
		switch arg(1) {
		case "restart", "try-restart":
			return "Restarting systemd service"
		case "enable", "start":
			return "Starting systemd service"
		case "stop":
			return "Stopping systemd service"
		default:
			return "Applying systemd service change"
		}
	case "certbot", "snap":
		return "Preparing HTTPS certificate tools"
	case "add-apt-repository":
		return "Adding reviewed package source"
	case "wg-guard":
		return "Applying initial panel settings"
	}
	return "Running installation task"
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
