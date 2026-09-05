package install

import (
	"context"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/subprocess"
)

// Host is the machine seam for the installer: filesystem, processes and
// network probes behind one interface so the whole install/update/uninstall
// flow runs in tests against an in-memory host.
type Host interface {
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
	if src == dst {
		return nil // self-install: the binary already lives at the destination
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	// Write via a sibling temp file + rename: overwriting the CURRENTLY
	// RUNNING binary in place fails with ETXTBSY on Linux, while rename
	// over it is allowed.
	tmp := dst + ".wg-install.tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
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

// memHost is the in-memory Host for tests: fs in a map, commands recorded
// with scripted results.
type memHost struct {
	files    map[string]memFile
	dirs     map[string]bool
	commands []memCmd
	failCmd  map[string]error  // first argv element → forced error
	output   map[string]string // first argv element → scripted stdout for Output
	portFree func(string) bool
	now      func() time.Time
}

type memFile struct {
	data []byte
	perm fs.FileMode
}

type memCmd struct {
	argv  []string
	stdin []byte // RunWithInput payload (secrets land here, never in argv)
}

func newMemHost() *memHost {
	return &memHost{
		files:   map[string]memFile{"/src/wg-guard": {data: []byte("/src/wg-guard"), perm: 0o755}, "/etc/os-release": {data: []byte("ID=ubuntu\nVERSION_ID=24.04\n")}, "/proc/1/comm": {data: []byte("systemd\n")}},
		dirs:    map[string]bool{},
		failCmd: map[string]error{},
		output:  map[string]string{"uname -s": "Linux", "uname -m": "x86_64", "uname -r": "6.8.0-138-generic", "modinfo": "MATCHINGBUILD", "awg": "amneziawg-tools v3.1.20260812", "ip": `[{"addr_info":[{"local":"203.0.113.7"}]}]`},
		now:     func() time.Time { return time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC) },
	}
}

func (m *memHost) Run(_ context.Context, argv []string, _ time.Duration) error {
	m.commands = append(m.commands, memCmd{argv: argv})
	if err := m.failCmd[argv[0]]; err != nil {
		return err
	}
	if argv[0] == "modprobe" && len(argv) == 2 {
		m.files["/sys/module/amneziawg/version"] = memFile{data: []byte("3.1.20260812")}
		m.files["/sys/module/amneziawg/srcversion"] = memFile{data: []byte("MATCHINGBUILD")}
	}
	return nil
}

// RunWithInput records the argv and the stdin payload separately, so tests
// can assert a secret was transported via stdin and never via argv.
func (m *memHost) RunWithInput(_ context.Context, argv []string, stdin io.Reader, _ time.Duration) error {
	data, _ := io.ReadAll(stdin)
	m.commands = append(m.commands, memCmd{argv: argv, stdin: data})
	if err := m.failCmd[argv[0]]; err != nil {
		return err
	}
	return nil
}

func (m *memHost) Output(ctx context.Context, argv []string, timeout time.Duration) (string, error) {
	m.commands = append(m.commands, memCmd{argv: argv})
	if err := m.failCmd[argv[0]]; err != nil {
		return "", err
	}
	if value, ok := m.output[argv[0]]; ok {
		return value, nil
	}
	if value, ok := m.output[strings.Join(argv, " ")]; ok {
		return value, nil
	}
	if argv[0] == "dpkg-query" {
		switch argv[len(argv)-1] {
		case "amneziawg-tools":
			return "installed\t1.0.20210914-0~202608130144+ee0f0a9~ubuntu24.04.1", nil
		case "amneziawg-dkms":
			return "installed\t1.0.0-0~202608282205+3c38e16~ubuntu24.04.1", nil
		default:
			return "installed\tsystem", nil
		}
	}
	return m.output[argv[0]], nil
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
