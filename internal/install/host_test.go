package install

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os/exec"
	"strings"
	"time"
)

// memHost is the in-memory Host for tests: fs in a map, commands recorded
// with scripted results.
type memHost struct {
	locked   bool
	files    map[string]memFile
	dirs     map[string]bool
	commands []memCmd
	failCmd  map[string]error  // first argv element → forced error
	output   map[string]string // first argv element → scripted stdout for Output
	portFree func(string) bool
	now      func() time.Time
}

func (m *memHost) LockLifecycle() (func(), error) {
	if m.locked {
		return nil, terminalError("install.error.lock")
	}
	m.locked = true
	return func() { m.locked = false }, nil
}
func (m *memHost) Open(p string) (io.ReadCloser, error) {
	b, e := m.ReadFile(p)
	return io.NopCloser(bytes.NewReader(b)), e
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
		output:  map[string]string{"uname -s": "Linux", "uname -m": "x86_64", "uname -r": "6.8.0-138-generic", "modinfo": "MATCHINGBUILD", "awg": "amneziawg-tools v3.1.20260812", "ip": `[{"addr_info":[{"local":"8.8.8.8"}]}]`},
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
	if len(argv) > 0 && argv[len(argv)-1] == "installer-contract" {
		return `{"revision":1,"data_contract":"schema7-h-ranges-v1","prerequisites":true,"recovery":true,"local_owner":true,"coordinated_restore":false}`, nil
	}
	if len(argv) > 1 && argv[1] == "owner-bootstrap" {
		return "present\n", nil
	}
	if len(argv) > 2 && argv[0] == "docker" && (argv[1] == "inspect" || argv[1] == "image" && argv[2] == "inspect") {
		return "sha256:" + strings.Repeat("a", 64), nil
	}
	if len(argv) > 6 && argv[0] == "docker" && argv[1] == "run" && argv[6] == "sha256sum" {
		return fmt.Sprintf("%x  %s", sha256.Sum256(m.files["/src/wg-guard"].data), BinPath), nil
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
	if strings.Join(argv, " ") == "systemctl show wg-guard.service --property=LoadState --property=ActiveState" {
		return "LoadState=loaded\nActiveState=inactive\n", nil
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
