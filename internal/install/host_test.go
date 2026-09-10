package install

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestRealHostStreamReturnsContextCancellation(t *testing.T) {
	if os.Getenv("WGG_TEST_STREAM_HELPER") == "1" {
		_, _ = fmt.Fprintln(os.Stdout, "ready")
		for {
			time.Sleep(time.Hour)
		}
	}
	t.Setenv("WGG_TEST_STREAM_HELPER", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	var stdout bytes.Buffer
	err := (realHost{}).Stream(ctx, []string{os.Args[0], "-test.run=^TestRealHostStreamReturnsContextCancellation$"}, &stdout, io.Discard)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("stream cancellation = %v", err)
	}
	if !strings.Contains(stdout.String(), "ready") {
		t.Fatalf("stream did not connect stdout: %q", stdout.String())
	}
}

// memHost is the in-memory Host for tests: fs in a map, commands recorded
// with scripted results.
type memHost struct {
	locked   bool
	files    map[string]memFile
	dirs     map[string]bool
	commands []memCmd
	failCmd  map[string]error  // first argv element → forced error
	output   map[string]string // first argv element → scripted stdout for Output
	dkms     map[string]bool
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
		dkms:    map[string]bool{},
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
	if len(argv) >= 3 && argv[0] == "make" && argv[1] == "-C" && strings.Contains(argv[2], "/tools/src") {
		built := argv[2] + "/wg"
		if argv[len(argv)-1] == "clean" {
			delete(m.files, built)
		} else {
			m.files[built] = memFile{data: []byte("reviewed awg binary"), perm: 0o755}
		}
	}
	if len(argv) > 1 && argv[0] == "dkms" && argv[1] == "install" {
		version, kernel := argumentAfter(argv, "-v"), argumentAfter(argv, "-k")
		m.dkms[version+"|"+kernel] = true
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
	if len(argv) >= 5 && argv[0] == "git" && argv[1] == "-C" && argv[3] == "rev-parse" {
		if strings.Contains(argv[2], "/tools") {
			return "ee0f0a9aa34ff0a0da4b3433b9512781cfe02843", nil
		}
		return "4569c4c67f3a57414969260cafbbd04694fbaae0", nil
	}
	if len(argv) > 1 && argv[0] == "dkms" && argv[1] == "status" {
		version, kernel := argumentAfter(argv, "-v"), argumentAfter(argv, "-k")
		if kernel != "" && m.dkms[version+"|"+kernel] {
			return "amneziawg/" + version + ", " + kernel + ", x86_64: installed", nil
		}
		return "", fmt.Errorf("not installed")
	}
	if value, ok := m.output[argv[0]]; ok {
		return value, nil
	}
	if value, ok := m.output[strings.Join(argv, " ")]; ok {
		return value, nil
	}
	if len(argv) > 0 && argv[len(argv)-1] == "installer-contract" {
		return `{"revision":2,"data_contract":"schema7-h-ranges-v1","prerequisites":true,"recovery":true,"local_owner":true,"coordinated_restore":true,"data_lease":true,"persistent_manager":true,"secure_exposure":true}`, nil
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

func (m *memHost) Stream(ctx context.Context, argv []string, stdout, _ io.Writer) error {
	m.commands = append(m.commands, memCmd{argv: append([]string(nil), argv...)})
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := m.failCmd[argv[0]]; err != nil {
		return err
	}
	value := m.output[strings.Join(argv, " ")]
	if value == "" {
		value = m.output[argv[0]]
	}
	_, err := io.WriteString(stdout, value)
	return err
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
	if file, ok := m.files[path]; ok {
		return statInfo{perm: file.perm}, nil
	}
	if m.dirs[path] {
		return statInfo{dir: true}, nil
	}
	return nil, fs.ErrNotExist
}

type statInfo struct {
	dir  bool
	perm fs.FileMode
}

func (s statInfo) Name() string { return "x" }
func (s statInfo) Size() int64  { return 0 }
func (s statInfo) Mode() fs.FileMode {
	if s.perm == 0 {
		return 0o600
	}
	return s.perm
}
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
	for p, f := range m.files {
		if strings.HasPrefix(p, old+"/") {
			delete(m.files, p)
			m.files[new+strings.TrimPrefix(p, old)] = f
		}
	}
	for p := range m.dirs {
		if p == old || strings.HasPrefix(p, old+"/") {
			delete(m.dirs, p)
			m.dirs[new+strings.TrimPrefix(p, old)] = true
		}
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

func argumentAfter(argv []string, flag string) string {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == flag {
			return argv[i+1]
		}
	}
	return ""
}
