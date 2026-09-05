package amneziawg

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/subprocess"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel"
)

// fakeRunner is a queue-based scripted runner: tests push responses in exact
// call order and assert the queue drains. It also captures the content of any
// .conf file argument (setconf/syncconf read the rendered file, which the
// backend deletes right after the call).
type fakeRunner struct {
	mu    sync.Mutex
	calls [][]string
	steps []fakeStep
	files map[string]string
}

type fakeStep struct {
	stdout string
	stderr string
	err    error
}

func (f *fakeRunner) step(stdout string) { f.steps = append(f.steps, fakeStep{stdout: stdout}) }
func (f *fakeRunner) fail(stderr string) {
	f.steps = append(f.steps, fakeStep{stderr: stderr, err: &subprocess.ExitError{Name: "awg", ExitCode: 1, Stderr: stderr}})
}
func (f *fakeRunner) failIP(stderr string) {
	f.steps = append(f.steps, fakeStep{stderr: stderr, err: &subprocess.ExitError{Name: "ip", ExitCode: 1, Stderr: stderr}})
}

func (f *fakeRunner) Run(_ context.Context, argv []string) (subprocess.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, append([]string(nil), argv...))
	if len(f.steps) == 0 {
		return subprocess.Result{}, nil
	}
	s := f.steps[0]
	f.steps = f.steps[1:]
	// Capture rendered config files before the backend deletes them.
	if len(argv) >= 4 && (argv[1] == "setconf" || argv[1] == "syncconf") {
		if data, err := os.ReadFile(argv[3]); err == nil {
			if f.files == nil {
				f.files = map[string]string{}
			}
			f.files[argv[1]+"-"+argv[2]] = string(data)
		}
	}
	return subprocess.Result{Stdout: []byte(s.stdout), Stderr: []byte(s.stderr)}, s.err
}

func (f *fakeRunner) drained(t *testing.T) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.steps) != 0 {
		t.Fatalf("%d scripted steps not consumed", len(f.steps))
	}
}

func (f *fakeRunner) argvJoined() string {
	var sb strings.Builder
	for _, c := range f.calls {
		sb.WriteString(strings.Join(c, " "))
		sb.WriteString("\n")
	}
	return sb.String()
}

// confFile returns the captured content of the last rendered config file for
// iface (setconf or syncconf).
func (f *fakeRunner) confFile(t *testing.T, op, iface string) string {
	t.Helper()
	if v, ok := f.files[op+"-"+iface]; ok {
		return v
	}
	t.Fatalf("no captured %s file for %s", op, iface)
	return ""
}

// dumpResponse renders a canned `awg show <name> dump` body for the given
// config, matching the pinned 29-field format.
func dumpResponse(pub string, port int, o tunnel.Obfuscation, peers ...string) string {
	num := func(i int) string { return strconv.Itoa(i) }
	iVal := func(s string) string {
		if s == "" {
			return "(null)"
		}
		return s
	}
	fields := []string{
		"cHJpdmF0ZWtleWJhc2U2NGR1bW15ZGF0YQ==", pub, num(port),
		num(o.Jc), num(o.Jmin), num(o.Jmax), num(o.S1), num(o.S2), "0", "0",
		o.H1.String(), o.H2.String(), o.H3.String(), o.H4.String(),
		iVal(o.I1), iVal(o.I2), iVal(o.I3), iVal(o.I4), iVal(o.I5), "(none)",
		"0", "0", "0", "0", "0", "0", "off", "off", "off",
	}
	if len(fields) != 29 {
		panic("canned dump must have 29 fields")
	}
	out := strings.Join(fields, "\t")
	for _, p := range peers {
		out += "\n" + p
	}
	return out + "\n"
}

func plainZero() tunnel.Obfuscation { return tunnel.Obfuscation{} }

func TestCreateInterfaceSequence(t *testing.T) {
	f := &fakeRunner{}
	b := NewWithBinary(f, "awg")
	spec := tunnel.InterfaceSpec{
		Name: "awg0", PrivateKey: testPriv, ListenPort: 39411, MTU: 1420,
		Address: "10.8.0.1/24", Obfuscation: obf(),
	}
	pub, err := tunnel.PublicKeyFromPrivate(testPriv)
	if err != nil {
		t.Fatal(err)
	}
	f.step("")                              // ip link add
	f.step("")                              // ip link set mtu
	f.step("")                              // setconf
	f.step(dumpResponse(pub, 39411, obf())) // dump (verify)
	f.step("")                              // ip addr add
	f.step("")                              // ip link set up
	if err := b.CreateInterface(context.Background(), spec); err != nil {
		t.Fatalf("create: %v", err)
	}
	f.drained(t)
	want := "ip link add awg0 type amneziawg\n" +
		"ip link set dev awg0 mtu 1420\n" +
		"awg setconf awg0 <conf>\n" +
		"awg show awg0 dump\n" +
		"ip addr add 10.8.0.1/24 dev awg0\n" +
		"ip link set dev awg0 up\n"
	if got := normalizeConfs(f.argvJoined()); got != want {
		t.Fatalf("argv sequence:\n%s\nwant:\n%s", got, want)
	}
	conf := f.confFile(t, "setconf", "awg0")
	if !strings.Contains(conf, "PrivateKey = "+testPriv+"\n") ||
		!strings.Contains(conf, "ListenPort = 39411\n") ||
		!strings.Contains(conf, "Jc = 5\n") {
		t.Fatalf("rendered setconf wrong:\n%s", conf)
	}
}

// normalizeConfs hides the random temp path so sequences can be asserted.
func normalizeConfs(s string) string {
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		if strings.Contains(ln, ".conf") {
			parts := strings.Split(ln, " ")
			for j, p := range parts {
				if strings.HasSuffix(p, ".conf") {
					parts[j] = "<conf>"
				}
			}
			lines[i] = strings.Join(parts, " ")
		}
	}
	return strings.Join(lines, "\n")
}

func TestCreateInterfaceRollsBackWhenSetconfFails(t *testing.T) {
	f := &fakeRunner{}
	b := NewWithBinary(f, "awg")
	spec := tunnel.InterfaceSpec{Name: "awg0", PrivateKey: testPriv, ListenPort: 39411, Address: "10.8.0.1/24"}
	f.step("") // ip link add
	f.fail("Unable to modify interface: Invalid argument")
	f.failIP("") // ip link del (rollback)
	err := b.CreateInterface(context.Background(), spec)
	if err == nil || !strings.Contains(err.Error(), "configure awg0") {
		t.Fatalf("want configure error, got %v", err)
	}
	f.drained(t)
	if !strings.Contains(f.argvJoined(), "ip link del dev awg0") {
		t.Fatalf("rollback missing:\n%s", f.argvJoined())
	}
}

func TestCreateInterfaceRollsBackOnVerifyMismatch(t *testing.T) {
	f := &fakeRunner{}
	b := NewWithBinary(f, "awg")
	spec := tunnel.InterfaceSpec{Name: "awg0", PrivateKey: testPriv, ListenPort: 39411, Obfuscation: obf()}
	pub, _ := tunnel.PublicKeyFromPrivate(testPriv)
	f.step("") // ip link add
	f.step("") // setconf
	// Backend kept old obfuscation params: verify must fail → rollback.
	f.step(dumpResponse(pub, 39411, plainZero()))
	f.failIP("") // ip link del
	err := b.CreateInterface(context.Background(), spec)
	if err == nil || !strings.Contains(err.Error(), "obfuscation parameters did not take effect") {
		t.Fatalf("want verify failure, got %v", err)
	}
	if !strings.Contains(f.argvJoined(), "ip link del dev awg0") {
		t.Fatalf("rollback missing")
	}
}

func TestApplyInterfaceConfigVerifyMismatch(t *testing.T) {
	f := &fakeRunner{}
	b := NewWithBinary(f, "awg")
	cfg := tunnel.InterfaceConfig{PrivateKey: testPriv, ListenPort: 39411, Obfuscation: obf()}
	pub, _ := tunnel.PublicKeyFromPrivate(testPriv)
	f.step("")                              // setconf
	f.step(dumpResponse(pub, 40000, obf())) // wrong port
	err := b.ApplyInterfaceConfig(context.Background(), "awg0", cfg)
	if err == nil || !strings.Contains(err.Error(), "listen port is 40000, want 39411") {
		t.Fatalf("want port verify failure, got %v", err)
	}
}

func TestApplyInterfaceConfigKeyMismatch(t *testing.T) {
	f := &fakeRunner{}
	b := NewWithBinary(f, "awg")
	cfg := tunnel.InterfaceConfig{PrivateKey: testPriv, ListenPort: 39411}
	f.step("") // setconf
	f.step(dumpResponse("other-public-key=", 39411, plainZero()))
	err := b.ApplyInterfaceConfig(context.Background(), "awg0", cfg)
	if err == nil || !strings.Contains(err.Error(), "public key did not take effect") {
		t.Fatalf("want key verify failure, got %v", err)
	}
}

func TestSyncPeers(t *testing.T) {
	f := &fakeRunner{}
	b := NewWithBinary(f, "awg")
	current := "[Interface]\nPrivateKey = " + testPriv + "\nListenPort = 39411\nJc = 5\n\n" +
		"[Peer]\nPublicKey = old-peer-key=\nAllowedIPs = 10.8.0.9/32\n"
	f.step(current) // showconf before sync
	f.step("")      // syncconf
	f.step(current) // showconf after sync; only its interface section must match
	peers := []tunnel.PeerConfig{{PublicKey: testPub, AllowedIPs: []string{"10.8.0.2/32"}}}
	if err := b.SyncPeers(context.Background(), "awg0", peers); err != nil {
		t.Fatalf("sync: %v", err)
	}
	f.drained(t)
	wantCalls := "awg showconf awg0\nawg syncconf awg0 <conf>\nawg showconf awg0\n"
	if got := normalizeConfs(f.argvJoined()); got != wantCalls {
		t.Fatalf("argv =\n%s", f.argvJoined())
	}
	conf := f.confFile(t, "syncconf", "awg0")
	if !strings.Contains(conf, "[Interface]\nPrivateKey = "+testPriv+"\n") ||
		!strings.Contains(conf, "ListenPort = 39411\n") ||
		!strings.Contains(conf, "Jc = 5\n") ||
		!strings.Contains(conf, "[Peer]\nPublicKey = "+testPub+"\n") ||
		!strings.Contains(conf, "AllowedIPs = 10.8.0.2/32\n") {
		t.Fatalf("rendered syncconf wrong:\n%s", conf)
	}
	if strings.Contains(conf, "old-peer-key=") {
		t.Fatalf("syncconf retained a stale peer:\n%s", conf)
	}
}

func TestSyncPeersRejectsMissingInterfaceSection(t *testing.T) {
	f := &fakeRunner{}
	b := NewWithBinary(f, "awg")
	f.step("[Peer]\nPublicKey = old-peer-key=\n")
	err := b.SyncPeers(context.Background(), "awg0", nil)
	if err == nil || !strings.Contains(err.Error(), "current interface configuration") {
		t.Fatalf("want safe interface-section error, got %v", err)
	}
	if got := f.argvJoined(); got != "awg showconf awg0\n" {
		t.Fatalf("unsafe sync continued:\n%s", got)
	}
}

func TestDumpNotFoundMapped(t *testing.T) {
	f := &fakeRunner{}
	b := NewWithBinary(f, "awg")
	f.fail("Unable to access interface: No such device")
	_, err := b.Dump(context.Background(), "awg7")
	if !errors.Is(err, tunnel.ErrInterfaceNotFound) {
		t.Fatalf("want ErrInterfaceNotFound, got %v", err)
	}
}

func TestListInterfaces(t *testing.T) {
	f := &fakeRunner{}
	b := NewWithBinary(f, "awg")
	// Pinned `awg show interfaces` prints all names on one space-separated
	// line. Newline-only parsing silently turns multiple interfaces into one
	// unknown name and can make reconciliation remove/recreate the wrong state.
	f.step("awg0 awg1\n")
	names, err := b.ListInterfaces(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || names[0] != "awg0" || names[1] != "awg1" {
		t.Fatalf("names = %v", names)
	}
	// Empty output (no interfaces) is not an error (pinned fact).
	f.step("")
	names, err = b.ListInterfaces(context.Background())
	if err != nil || len(names) != 0 {
		t.Fatalf("empty list: %v %v", names, err)
	}
}

func TestRemoveInterface(t *testing.T) {
	f := &fakeRunner{}
	b := NewWithBinary(f, "awg")
	f.step("")
	if err := b.RemoveInterface(context.Background(), "awg0"); err != nil {
		t.Fatal(err)
	}
	// Missing link maps to the backend's canonical error.
	f.failIP("Device \"awg9\" does not exist.")
	err := b.RemoveInterface(context.Background(), "awg9")
	if !errors.Is(err, tunnel.ErrInterfaceNotFound) {
		t.Fatalf("want ErrInterfaceNotFound, got %v", err)
	}
}

func TestToolsVersion(t *testing.T) {
	f := &fakeRunner{}
	b := NewWithBinary(f, "awg")
	f.step("amneziawg-tools v3.1.20260812\n")
	v, err := b.ToolsVersion(context.Background())
	if err != nil || v != PinnedToolsVersion {
		t.Fatalf("v=%q err=%v", v, err)
	}
	f.step("garbage\n")
	if _, err := b.ToolsVersion(context.Background()); err == nil {
		t.Fatal("want error on unparseable version")
	}
	f.fail("")
	if _, err := b.ToolsVersion(context.Background()); err == nil {
		t.Fatal("want error when awg missing")
	}
}
