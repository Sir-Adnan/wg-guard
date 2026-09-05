// Package subprocess is WG-Guard's single choke point for executing external
// programs (awg, ip, nft, sysctl — ADR-0001). Centralizing exec here makes the
// security properties auditable in one place: explicit argv only (no shell,
// therefore no interpolation), bounded runtime via a default timeout, and a
// structured exit error that never embeds stdout — command output can contain
// key material (e.g. `awg show dump`) and must never be logged.
package subprocess

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// DefaultTimeout bounds one command when the caller's context has no earlier
// deadline. Every command WG-Guard runs (awg config ops, ip link, nft -f,
// sysctl) completes in well under a second on a healthy host; 15s covers
// loaded systems without masking a hung helper as success.
const DefaultTimeout = 15 * time.Second

// Result is the captured output of one command. Callers parse it; they never
// log it (stdout may contain private keys).
type Result struct {
	Stdout []byte
	Stderr []byte
}

// ExitError describes a non-zero exit. Stderr (truncated) is safe to surface:
// pinned tools report operational failures there, not configuration content.
type ExitError struct {
	Name     string // argv[0]
	ExitCode int
	Stderr   string
}

func (e *ExitError) Error() string {
	msg := strings.TrimSpace(e.Stderr)
	if len(msg) > 300 {
		msg = msg[:300] + "…"
	}
	if msg == "" {
		return fmt.Sprintf("%s exited with status %d", e.Name, e.ExitCode)
	}
	return fmt.Sprintf("%s exited with status %d: %s", e.Name, e.ExitCode, msg)
}

// Runner executes one command per call. Implementations must be safe for
// concurrent use.
type Runner interface {
	// Run executes argv (argv[0] is the binary; the rest are arguments, in
	// order, passed to the process untouched). On a non-zero exit it returns
	// the captured output alongside an *ExitError so callers can classify
	// failures by stderr. A missing binary surfaces exec.ErrNotFound.
	Run(ctx context.Context, argv []string) (Result, error)
}

// System runs real binaries through os/exec.
type System struct {
	// Timeout bounds a command when ctx has no earlier deadline. Zero uses
	// DefaultTimeout.
	Timeout time.Duration
}

// NewSystem returns a System runner with the default timeout.
func NewSystem() *System { return &System{Timeout: DefaultTimeout} }

func (s *System) Run(ctx context.Context, argv []string) (Result, error) {
	return s.run(ctx, argv, "", nil)
}

func (s *System) run(ctx context.Context, argv []string, dir string, env []string) (Result, error) {
	if len(argv) == 0 {
		return Result{}, fmt.Errorf("subprocess: empty argv")
	}
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...) // explicit argv — never a shell
	cmd.Dir = dir
	cmd.Env = env
	cmd.WaitDelay = time.Second
	var stdout, stderr boundedOutput
	if env != nil {
		stdout.limit = 1 << 20
		stderr.limit = 1 << 20
	}
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	res := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	if err != nil {
		// A deadline/cancel kill masquerades as a signaled exit; report the
		// context error instead of a bogus exit status (-1).
		if ctxErr := runCtx.Err(); ctxErr != nil {
			return res, fmt.Errorf("subprocess: run %s: %w", argv[0], ctxErr)
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return res, &ExitError{Name: argv[0], ExitCode: exitErr.ExitCode(), Stderr: string(res.Stderr)}
		}
		// Exec failure (binary missing) — not an exit.
		return res, fmt.Errorf("subprocess: run %s: %w", argv[0], err)
	}
	return res, nil
}
