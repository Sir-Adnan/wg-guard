package firewall

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/subprocess"
)

type fakeRunner struct {
	mu      sync.Mutex
	calls   [][]string
	steps   []fakeStep
	scripts []string // nft -f files, in capture order
}

type fakeStep struct {
	stdout string
	stderr string
	err    error
}

func (f *fakeRunner) run(stdout string) { f.steps = append(f.steps, fakeStep{stdout: stdout}) }
func (f *fakeRunner) fail(stderr string) {
	f.steps = append(f.steps, fakeStep{stderr: stderr, err: &subprocess.ExitError{Name: "nft", ExitCode: 1, Stderr: stderr}})
}
func (f *fakeRunner) failCmd(name, stderr string) {
	f.steps = append(f.steps, fakeStep{stderr: stderr, err: &subprocess.ExitError{Name: name, ExitCode: 1, Stderr: stderr}})
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
	if len(argv) >= 3 && argv[0] == "nft" && argv[1] == "-f" {
		if data, err := os.ReadFile(argv[2]); err == nil {
			f.scripts = append(f.scripts, string(data))
		}
	}
	return subprocess.Result{Stdout: []byte(s.stdout), Stderr: []byte(s.stderr)}, s.err
}

func (f *fakeRunner) consumed(t *testing.T) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.steps) != 0 {
		t.Fatalf("%d steps not consumed", len(f.steps))
	}
}

func (f *fakeRunner) lastScript(t *testing.T) string {
	t.Helper()
	if len(f.scripts) == 0 {
		t.Fatal("no captured nft script")
	}
	return f.scripts[len(f.scripts)-1]
}

func TestRenderTableGolden(t *testing.T) {
	ifaces := []Interface{
		{Name: "awg0", Subnet: "10.8.0.0/24"},
		{Name: "awg1", Subnet: "10.8.1.0/24"},
	}
	want := `table inet wgguard {
	chain forward {
		type filter hook forward priority 10; policy accept;
		iifname "awg0" accept comment "wgguard:managed:awg0"
		oifname "awg0" accept comment "wgguard:managed:awg0"
		iifname "awg1" accept comment "wgguard:managed:awg1"
		oifname "awg1" accept comment "wgguard:managed:awg1"
	}
	chain postrouting {
		type nat hook postrouting priority 100; policy accept;
		oifname != "awg0" ip saddr 10.8.0.0/24 masquerade comment "wgguard:managed:awg0"
		oifname != "awg1" ip saddr 10.8.1.0/24 masquerade comment "wgguard:managed:awg1"
	}
}
`
	if got := string(renderTable(ifaces, false)); got != want {
		t.Fatalf("render:\n%s\nwant:\n%s", got, want)
	}
	if !strings.HasPrefix(string(renderTable(ifaces, true)), "delete table inet wgguard\n") {
		t.Fatal("dropExisting must lead with the delete")
	}
}

func TestApplyReplacesExistingTable(t *testing.T) {
	f := &fakeRunner{}
	m := &Manager{Run: f}
	f.run("nft version 1.0.x")                 // nft --version (via Present)
	f.fail("Error: No such file or directory") // nft list table → absent
	f.run("")                                  // nft -f
	if err := m.Apply(context.Background(), []Interface{{Name: "awg0", Subnet: "10.8.0.0/24"}}); err != nil {
		t.Fatal(err)
	}
	f.consumed(t)
	script := f.lastScript(t)
	if strings.HasPrefix(script, "delete table") {
		t.Fatalf("table absent: create-only script expected:\n%s", script)
	}

	// Second apply: table now present → atomic delete + recreate.
	f.run("nft version 1.0.x")        // --version (via Present)
	f.run("table inet wgguard {...}") // probe → present
	f.run("")                         // nft -f
	if err := m.Apply(context.Background(), []Interface{{Name: "awg0", Subnet: "10.8.0.0/24"}}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(f.lastScript(t), "delete table inet wgguard\n") {
		t.Fatalf("existing table must be deleted first:\n%s", f.lastScript(t))
	}
}

func TestApplyWithoutNftFailsClearly(t *testing.T) {
	f := &fakeRunner{}
	m := &Manager{Run: f}
	// Missing binary: runner returns the exec.ErrNotFound shape.
	f.mu.Lock()
	f.steps = []fakeStep{{err: fmtExitNotFound()}}
	f.mu.Unlock()
	err := m.Apply(context.Background(), []Interface{{Name: "awg0", Subnet: "10.8.0.0/24"}})
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("want clear nft-missing error, got %v", err)
	}
}

func TestApplyEmptyRemovesTable(t *testing.T) {
	f := &fakeRunner{}
	m := &Manager{Run: f}
	f.run("nft version") // --version (Remove → nftAvailable)
	f.fail("Error: No such file or directory")
	if err := m.Apply(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	f.consumed(t)
	if !strings.Contains(f.callsJoined(), "nft delete table inet wgguard") {
		t.Fatalf("argv:\n%s", f.callsJoined())
	}
}

func TestRemoveIdempotent(t *testing.T) {
	f := &fakeRunner{}
	m := &Manager{Run: f}
	f.run("nft version")
	f.fail("Error: No such file or directory") // absent → success
	if err := m.Remove(context.Background()); err != nil {
		t.Fatalf("remove absent table must succeed, got %v", err)
	}
}

func TestCoexistenceUfw(t *testing.T) {
	ctx := context.Background()

	t.Run("active with deny routed policy", func(t *testing.T) {
		f := &fakeRunner{}
		m := &Manager{Run: f}
		f.run("Status: active\nDefault: deny (incoming), allow (outgoing), deny (routed)\nNew profiles: skip\n")
		f.failCmd("firewall-cmd", "not running") // firewalld absent
		findings, err := m.Coexistence(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 1 || findings[0].Tool != "ufw" || !findings[0].Blocking {
			t.Fatalf("findings = %+v", findings)
		}
	})

	t.Run("active with disabled routed policy", func(t *testing.T) {
		f := &fakeRunner{}
		m := &Manager{Run: f}
		f.run("Status: active\nDefault: deny (incoming), allow (outgoing), disabled (routed)\n")
		f.failCmd("firewall-cmd", "not running")
		findings, err := m.Coexistence(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 1 || findings[0].Blocking {
			t.Fatalf("findings = %+v", findings)
		}
	})

	t.Run("not installed", func(t *testing.T) {
		f := &fakeRunner{}
		m := &Manager{Run: f}
		f.failCmd("ufw", "command not found")
		f.failCmd("firewall-cmd", "command not found")
		findings, err := m.Coexistence(ctx)
		if err != nil || len(findings) != 0 {
			t.Fatalf("findings=%+v err=%v", findings, err)
		}
	})
}

func TestEnsureUfwRoutes(t *testing.T) {
	ctx := context.Background()

	t.Run("applies per interface when ufw active", func(t *testing.T) {
		f := &fakeRunner{}
		m := &Manager{Run: f}
		f.run("Status: active\n")
		f.run("Rule added\n")
		f.run("Rule added\n")
		applied, err := m.EnsureUfwRoutes(ctx, []Interface{{Name: "awg0"}, {Name: "awg1"}})
		if err != nil {
			t.Fatal(err)
		}
		if len(applied) != 2 {
			t.Fatalf("applied = %v", applied)
		}
		if !strings.Contains(f.callsJoined(), "ufw route allow in on awg0") ||
			!strings.Contains(f.callsJoined(), "ufw route allow in on awg1") {
			t.Fatalf("argv:\n%s", f.callsJoined())
		}
	})

	t.Run("no ufw is a no-op", func(t *testing.T) {
		f := &fakeRunner{}
		m := &Manager{Run: f}
		f.failCmd("ufw", "not found")
		applied, err := m.EnsureUfwRoutes(ctx, []Interface{{Name: "awg0"}})
		if err != nil || len(applied) != 0 {
			t.Fatalf("applied=%v err=%v", applied, err)
		}
	})
}

func (f *fakeRunner) callsJoined() string {
	var sb strings.Builder
	for _, c := range f.calls {
		sb.WriteString(strings.Join(c, " "))
		sb.WriteString("\n")
	}
	return sb.String()
}

// fmtExitNotFound builds the exec-not-found error shape the real runner
// produces for a missing binary.
func fmtExitNotFound() error {
	return &exec.Error{Name: "nft", Err: exec.ErrNotFound}
}
