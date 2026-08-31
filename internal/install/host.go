package install

import (
	"context"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"runtime"
	"time"
)

// Host is the machine seam for the installer: filesystem, processes and
// network probes behind one interface so the whole install/update/uninstall
// flow runs in tests against an in-memory host.
type Host interface {
	// Run executes argv (explicit argv only, no shell — subprocess policy).
	// A non-zero exit returns an error describing the failure (stderr is
	// safe: installer commands never touch key material).
	Run(ctx context.Context, argv []string, timeout time.Duration) error
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

func (realHost) LookPath(name string) (string, error) { return exec.LookPath(name) }
func (realHost) MkdirAll(path string, perm fs.FileMode) error {
	return os.MkdirAll(path, perm)
}
func (realHost) WriteFile(path string, data []byte, perm fs.FileMode) error {
	return os.WriteFile(path, data, perm)
}
func (realHost) ReadFile(path string) ([]byte, error)  { return os.ReadFile(path) }
func (realHost) Stat(path string) (fs.FileInfo, error) { return os.Stat(path) }
func (realHost) Remove(path string) error              { return os.Remove(path) }
func (realHost) RemoveAll(path string) error           { return os.RemoveAll(path) }
func (realHost) Rename(old, new string) error          { return os.Rename(old, new) }

func (realHost) CopyFile(src, dst string, perm fs.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, perm)
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
	return net.LookupHost(hostname)
}

// memHost is the in-memory Host for tests: fs in a map, commands recorded
// with scripted results.
type memHost struct {
	files    map[string]memFile
	dirs     map[string]bool
	commands []memCmd
	failCmd  map[string]error // first argv element → forced error
	portFree func(string) bool
	now      func() time.Time
}

type memFile struct {
	data []byte
	perm fs.FileMode
}

type memCmd struct {
	argv []string
}

func newMemHost() *memHost {
	return &memHost{
		files:   map[string]memFile{"/src/wg-guard": {data: []byte("/src/wg-guard"), perm: 0o755}},
		dirs:    map[string]bool{},
		failCmd: map[string]error{},
		now:     func() time.Time { return time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC) },
	}
}

func (m *memHost) Run(_ context.Context, argv []string, _ time.Duration) error {
	m.commands = append(m.commands, memCmd{argv: argv})
	if err := m.failCmd[argv[0]]; err != nil {
		return err
	}
	return nil
}

func (m *memHost) LookPath(name string) (string, error) {
	if _, ok := m.failCmd[name]; ok {
		return "", exec.ErrNotFound
	}
	return "/usr/bin/" + name, nil
}

func (m *memHost) MkdirAll(path string, _ fs.FileMode) error {
	m.dirs[path] = true
	return nil
}

func (m *memHost) WriteFile(path string, data []byte, perm fs.FileMode) error {
	m.files[path] = memFile{data: append([]byte(nil), data...), perm: perm}
	return nil
}

func (m *memHost) ReadFile(path string) ([]byte, error) {
	if f, ok := m.files[path]; ok {
		return append([]byte(nil), f.data...), nil
	}
	return nil, fs.ErrNotExist
}

func (m *memHost) Stat(path string) (fs.FileInfo, error) {
	if _, ok := m.files[path]; ok {
		return statInfo{}, nil
	}
	if m.dirs[path] {
		return statInfo{dir: true}, nil
	}
	return nil, fs.ErrNotExist
}

type statInfo struct{ dir bool }

func (s statInfo) Name() string       { return "x" }
func (s statInfo) Size() int64        { return 0 }
func (s statInfo) Mode() fs.FileMode  { return 0o600 }
func (s statInfo) ModTime() time.Time { return time.Time{} }
func (s statInfo) IsDir() bool        { return s.dir }
func (s statInfo) Sys() any           { return nil }

func (m *memHost) Remove(path string) error {
	delete(m.files, path)
	return nil
}

func (m *memHost) RemoveAll(path string) error {
	delete(m.files, path)
	delete(m.dirs, path)
	return nil
}

func (m *memHost) Rename(old, new string) error {
	if f, ok := m.files[old]; ok {
		delete(m.files, old)
		m.files[new] = f
	}
	return nil
}

func (m *memHost) CopyFile(src, dst string, perm fs.FileMode) error {
	data, err := m.ReadFile(src)
	if err != nil {
		return err
	}
	return m.WriteFile(dst, data, perm)
}

func (m *memHost) SelfExe() (string, error) { return "/src/wg-guard", nil }
func (m *memHost) IsRoot() bool             { return true }
func (m *memHost) PortFree(addr string) bool {
	if m.portFree != nil {
		return m.portFree(addr)
	}
	return true
}
func (m *memHost) LookupHost(string) ([]string, error) {
	return []string{"203.0.113.7"}, nil
}

// ranCommands flattens recorded commands for assertions.
func (m *memHost) ranCommands() [][]string {
	out := make([][]string, 0, len(m.commands))
	for _, c := range m.commands {
		out = append(out, c.argv)
	}
	return out
}

// ranAll reports whether every argv prefix was executed in order.
func (m *memHost) ran(prefixes ...string) bool {
	for _, argv := range m.ranCommands() {
		if len(argv) > 0 && len(prefixes) > 0 && argv[0] == prefixes[0] {
			rest := prefixes[1:]
			if len(argv)-1 < len(rest) {
				continue
			}
			match := true
			for i, want := range rest {
				if argv[1+i] != want {
					match = false
					break
				}
			}
			if match {
				return true
			}
		}
	}
	return false
}

var _ Host = (*memHost)(nil)
var _ Host = realHost{}
