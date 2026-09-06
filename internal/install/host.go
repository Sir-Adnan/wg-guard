package install

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/subprocess"
)

// Host is the machine seam for the installer: filesystem, processes and
// network probes behind one interface so the whole install/update/uninstall
// flow runs in tests against an in-memory host.
type Host interface {
	LockLifecycle() (func(), error)
	Open(path string) (io.ReadCloser, error)
	// Run executes argv (explicit argv only, no shell — subprocess policy).
	// A non-zero exit returns an error describing the failure (stderr is
	// safe: installer commands never touch key material).
	Run(ctx context.Context, argv []string, timeout time.Duration) error
	// RunWithInput runs argv feeding stdin from r. Secrets (settings values,
	// passphrases) travel this way — never via argv (security.md).
	RunWithInput(ctx context.Context, argv []string, stdin io.Reader, timeout time.Duration) error
	// Output runs argv and captures stdout (status formatting needs the
	// command's value, not just its exit status).
	Output(ctx context.Context, argv []string, timeout time.Duration) (string, error)
	LookPath(name string) (string, error)

	MkdirAll(path string, perm fs.FileMode) error
	WriteFile(path string, data []byte, perm fs.FileMode) error
	ReadFile(path string) ([]byte, error)
	Stat(path string) (fs.FileInfo, error)
	Remove(path string) error
	RemoveAll(path string) error
	Rename(old, new string) error
	CopyFile(src, dst string, perm fs.FileMode) error

	// SelfExe is the path of the running wg-guard binary (native installs
	// copy it into /usr/local/bin).
	SelfExe() (string, error)
	IsRoot() bool

	// PortFree reports whether a listener can bind addr right now.
	PortFree(addr string) bool
	// LookupHost resolves a hostname (ACME preflight).
	LookupHost(hostname string) ([]string, error)
}

// realHost is the production Host.
type realHost struct{}

// NewRealHost returns the host seam backed by the real machine.
func NewRealHost() Host { return realHost{} }

func (realHost) Run(ctx context.Context, argv []string, timeout time.Duration) error {
	runCtx := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...) //nolint:gosec // explicit argv, installer-controlled
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func (realHost) RunWithInput(ctx context.Context, argv []string, stdin io.Reader, timeout time.Duration) error {
	runCtx := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...) //nolint:gosec // explicit argv, installer-controlled
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = stdin
	return cmd.Run()
}

func (realHost) Output(ctx context.Context, argv []string, timeout time.Duration) (string, error) {
	runner := &subprocess.System{Timeout: timeout}
	result, err := runner.RunConfigured(ctx, argv, "", os.Environ())
	return string(result.Stdout), err
}

func (realHost) LookPath(name string) (string, error) { return exec.LookPath(name) }
func (realHost) MkdirAll(path string, perm fs.FileMode) error {
	if err := safeHostPath(path); err != nil {
		return err
	}
	return os.MkdirAll(path, perm)
}
func (realHost) WriteFile(path string, data []byte, perm fs.FileMode) error {
	return os.WriteFile(path, data, perm)
}
func (realHost) ReadFile(path string) ([]byte, error)    { return os.ReadFile(path) }
func (realHost) Open(path string) (io.ReadCloser, error) { return os.Open(path) }
func (realHost) Stat(path string) (fs.FileInfo, error)   { return os.Stat(path) }
func (realHost) Remove(path string) error                { return os.Remove(path) }
func (realHost) RemoveAll(path string) error             { return os.RemoveAll(path) }
func (realHost) Rename(old, new string) error            { return os.Rename(old, new) }

func (realHost) CopyFile(src, dst string, perm fs.FileMode) error {
	if src == dst {
		return nil // self-install: the binary already lives at the destination
	}
	if err := safeHostPath(src); err != nil {
		return err
	}
	if err := safeHostPath(dst); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > 256<<20 {
		return terminalError("install.error.binary")
	}
	f, err := os.CreateTemp(filepath.Dir(dst), ".wg-guard-binary-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(perm); err == nil {
		var n int64
		n, err = io.Copy(f, io.LimitReader(in, 256<<20+1))
		if n > 256<<20 {
			err = terminalError("install.error.binary")
		}
	}
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	if err = os.Rename(f.Name(), dst); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(dst))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func (realHost) SelfExe() (string, error) { return os.Executable() }
func (realHost) IsRoot() bool             { return runtime.GOOS != "windows" && os.Geteuid() == 0 }

func (realHost) PortFree(addr string) bool {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

func (realHost) LookupHost(hostname string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return net.DefaultResolver.LookupHost(ctx, hostname)
}

var _ Host = realHost{}
