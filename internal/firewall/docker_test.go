package firewall

import (
	"context"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/subprocess"
)

type dockerRunner struct {
	calls     []string
	responses map[string]fakeStep
}

func (r *dockerRunner) Run(_ context.Context, argv []string) (subprocess.Result, error) {
	joined := strings.Join(argv, " ")
	r.calls = append(r.calls, joined)
	step, ok := r.responses[joined]
	if !ok {
		return subprocess.Result{}, nil
	}
	return subprocess.Result{Stdout: []byte(step.stdout), Stderr: []byte(step.stderr)}, step.err
}

func (r *dockerRunner) index(call string) int {
	for i, got := range r.calls {
		if got == call {
			return i
		}
	}
	return -1
}

func TestEnsureDockerForwardingReconcilesWithoutFlushingLiveRules(t *testing.T) {
	const (
		oldIngress = "iptables -w 5 -D WGGUARD-FORWARD -s 10.8.9.0/24 -i awg9 -j ACCEPT"
		oldReturn  = "iptables -w 5 -D WGGUARD-FORWARD -d 10.8.9.0/24 -o awg9 -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT"
		newIngress = "iptables -w 5 -A WGGUARD-FORWARD -s 10.8.0.0/24 -i awg0 -j ACCEPT"
		newReturn  = "iptables -w 5 -A WGGUARD-FORWARD -d 10.8.0.0/24 -o awg0 -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT"
	)
	r := &dockerRunner{responses: map[string]fakeStep{
		"iptables --version":               {stdout: "iptables v1.8.10 (nf_tables)\n"},
		"iptables -w 5 -S DOCKER-USER":     {stdout: "-N DOCKER-USER\n-A DOCKER-USER -m comment --comment wgguard:managed:docker-forward -j WGGUARD-FORWARD\n-A DOCKER-USER -j RETURN\n"},
		"iptables -w 5 -S WGGUARD-FORWARD": {stdout: "-N WGGUARD-FORWARD\n-A WGGUARD-FORWARD -s 10.8.9.0/24 -i awg9 -j ACCEPT\n-A WGGUARD-FORWARD -d 10.8.9.0/24 -o awg9 -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT\n"},
	}}

	managed, err := (&Manager{Run: r}).EnsureDockerForwarding(context.Background(),
		[]Interface{{Name: "awg0", Subnet: "10.8.0.0/24"}})
	if err != nil || !managed {
		t.Fatalf("managed=%v err=%v", managed, err)
	}
	for _, forbidden := range r.calls {
		if forbidden == "iptables -w 5 -F WGGUARD-FORWARD" {
			t.Fatal("live compatibility chain was flushed instead of reconciled")
		}
	}
	for _, want := range []string{newIngress, newReturn, oldIngress, oldReturn} {
		if r.index(want) < 0 {
			t.Errorf("missing command %q; calls=%v", want, r.calls)
		}
	}
	if r.index(newIngress) > r.index(oldIngress) || r.index(newReturn) > r.index(oldReturn) {
		t.Fatalf("replacement rules must be admitted before stale rules are removed: %v", r.calls)
	}
}

func TestEnsureDockerForwardingNoDockerChainIsNoop(t *testing.T) {
	missing := &subprocess.ExitError{Name: "iptables", ExitCode: 1, Stderr: "iptables: No chain/target/match by that name."}
	r := &dockerRunner{responses: map[string]fakeStep{
		"iptables --version":           {stdout: "iptables v1.8.10 (nf_tables)\n"},
		"iptables -w 5 -S DOCKER-USER": {stderr: missing.Stderr, err: missing},
	}}
	managed, err := (&Manager{Run: r}).EnsureDockerForwarding(context.Background(),
		[]Interface{{Name: "awg0", Subnet: "10.8.0.0/24"}})
	if err != nil || managed {
		t.Fatalf("managed=%v err=%v", managed, err)
	}
	if len(r.calls) != 2 {
		t.Fatalf("host without Docker was mutated: %v", r.calls)
	}
}

func TestEnsureDockerForwardingEmptyStateRemovesOnlyOwnedChain(t *testing.T) {
	r := &dockerRunner{responses: map[string]fakeStep{
		"iptables --version":               {stdout: "iptables v1.8.10 (nf_tables)\n"},
		"iptables -w 5 -S DOCKER-USER":     {stdout: "-N DOCKER-USER\n-A DOCKER-USER -m comment --comment wgguard:managed:docker-forward -j WGGUARD-FORWARD\n-A DOCKER-USER -j RETURN\n"},
		"iptables -w 5 -S WGGUARD-FORWARD": {stdout: "-N WGGUARD-FORWARD\n"},
	}}
	managed, err := (&Manager{Run: r}).EnsureDockerForwarding(context.Background(), nil)
	if err != nil || !managed {
		t.Fatalf("managed=%v err=%v", managed, err)
	}
	for _, want := range []string{
		"iptables -w 5 -D DOCKER-USER -m comment --comment wgguard:managed:docker-forward -j WGGUARD-FORWARD",
		"iptables -w 5 -F WGGUARD-FORWARD",
		"iptables -w 5 -X WGGUARD-FORWARD",
	} {
		if r.index(want) < 0 {
			t.Errorf("missing owned cleanup %q; calls=%v", want, r.calls)
		}
	}
	for _, call := range r.calls {
		if strings.Contains(call, "-F DOCKER-USER") || strings.Contains(call, "-X DOCKER-USER") {
			t.Fatalf("foreign Docker chain mutated: %v", r.calls)
		}
	}
}

func TestCheckForwardingRejectsUnmanagedDrop(t *testing.T) {
	r := &dockerRunner{responses: map[string]fakeStep{
		"iptables --version":       {stdout: "iptables v1.8.10 (nf_tables)\n"},
		"iptables -w 5 -S FORWARD": {stdout: "-P FORWARD DROP\n-A FORWARD -j FOREIGN-FILTER\n"},
	}}
	status, err := (&Manager{Run: r}).CheckForwarding(context.Background(),
		[]Interface{{Name: "awg0", Subnet: "10.8.0.0/24"}}, false, nil)
	if err == nil || !strings.Contains(err.Error(), "FORWARD policy is DROP") {
		t.Fatalf("status=%+v err=%v", status, err)
	}
}

func TestCheckForwardingAcceptsManagedDrop(t *testing.T) {
	for _, tc := range []struct {
		name          string
		dockerManaged bool
		ufwRoutes     []string
	}{
		{name: "Docker", dockerManaged: true},
		{name: "UFW", ufwRoutes: []string{"awg0"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &dockerRunner{responses: map[string]fakeStep{
				"iptables --version":       {stdout: "iptables v1.8.10 (nf_tables)\n"},
				"iptables -w 5 -S FORWARD": {stdout: "-P FORWARD DROP\n"},
			}}
			status, err := (&Manager{Run: r}).CheckForwarding(context.Background(),
				[]Interface{{Name: "awg0", Subnet: "10.8.0.0/24"}}, tc.dockerManaged, tc.ufwRoutes)
			if err != nil || status.Policy != "DROP" || !status.Managed {
				t.Fatalf("status=%+v err=%v", status, err)
			}
		})
	}
}

func TestInspectForwardingDistinguishesCompleteAndPartialDockerPath(t *testing.T) {
	base := map[string]fakeStep{
		"iptables --version":               {stdout: "iptables v1.8.10 (nf_tables)\n"},
		"iptables -w 5 -S FORWARD":         {stdout: "-P FORWARD DROP\n-A FORWARD -j DOCKER-USER\n"},
		"iptables -w 5 -S DOCKER-USER":     {stdout: "-N DOCKER-USER\n-A DOCKER-USER -m comment --comment wgguard:managed:docker-forward -j WGGUARD-FORWARD\n-A DOCKER-USER -j RETURN\n"},
		"iptables -w 5 -S WGGUARD-FORWARD": {stdout: "-N WGGUARD-FORWARD\n-A WGGUARD-FORWARD -s 10.8.0.0/24 -i awg0 -j ACCEPT\n-A WGGUARD-FORWARD -d 10.8.0.0/24 -o awg0 -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT\n"},
	}
	r := &dockerRunner{responses: base}
	inspection, err := (&Manager{Run: r}).InspectForwarding(context.Background(),
		[]Interface{{Name: "awg0", Subnet: "10.8.0.0/24"}})
	if err != nil || inspection.Policy != "DROP" || !inspection.DockerUser || !inspection.DockerManaged {
		t.Fatalf("complete inspection=%+v err=%v", inspection, err)
	}

	partial := make(map[string]fakeStep, len(base))
	for key, value := range base {
		partial[key] = value
	}
	partial["iptables -w 5 -S WGGUARD-FORWARD"] = fakeStep{stdout: "-N WGGUARD-FORWARD\n-A WGGUARD-FORWARD -s 10.8.0.0/24 -i awg0 -j ACCEPT\n"}
	r = &dockerRunner{responses: partial}
	inspection, err = (&Manager{Run: r}).InspectForwarding(context.Background(),
		[]Interface{{Name: "awg0", Subnet: "10.8.0.0/24"}})
	if err != nil || !inspection.DockerUser || inspection.DockerManaged {
		t.Fatalf("partial inspection=%+v err=%v", inspection, err)
	}
}

func TestInspectForwardingAcceptsCanonicalIPTablesRuleOrdering(t *testing.T) {
	r := &dockerRunner{responses: map[string]fakeStep{
		"iptables --version":               {stdout: "iptables v1.8.10 (nf_tables)\n"},
		"iptables -w 5 -S FORWARD":         {stdout: "-P FORWARD DROP\n-A FORWARD -j DOCKER-USER\n"},
		"iptables -w 5 -S DOCKER-USER":     {stdout: "-N DOCKER-USER\n-A DOCKER-USER -m comment --comment \"wgguard:managed:docker-forward\" -j WGGUARD-FORWARD\n"},
		"iptables -w 5 -S WGGUARD-FORWARD": {stdout: "-N WGGUARD-FORWARD\n-A WGGUARD-FORWARD -s 10.8.0.0/24 -i awg0 -j ACCEPT\n-A WGGUARD-FORWARD -d 10.8.0.0/24 -o awg0 -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT\n"},
	}}
	inspection, err := (&Manager{Run: r}).InspectForwarding(context.Background(),
		[]Interface{{Name: "awg0", Subnet: "10.8.0.0/24"}})
	if err != nil || !inspection.DockerManaged {
		t.Fatalf("canonical live rules not recognized: %+v err=%v", inspection, err)
	}
}

func TestRemoveCleansNamespacedAndDockerCompatibilityState(t *testing.T) {
	r := &dockerRunner{responses: map[string]fakeStep{
		"iptables --version":               {stdout: "iptables v1.8.10 (nf_tables)\n"},
		"iptables -w 5 -S DOCKER-USER":     {stdout: "-N DOCKER-USER\n-A DOCKER-USER -m comment --comment wgguard:managed:docker-forward -j WGGUARD-FORWARD\n"},
		"iptables -w 5 -S WGGUARD-FORWARD": {stdout: "-N WGGUARD-FORWARD\n"},
	}}
	if err := (&Manager{Run: r}).Remove(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"nft delete table inet wgguard",
		"iptables -w 5 -D DOCKER-USER -m comment --comment wgguard:managed:docker-forward -j WGGUARD-FORWARD",
		"iptables -w 5 -X WGGUARD-FORWARD",
	} {
		if r.index(want) < 0 {
			t.Errorf("missing cleanup %q; calls=%v", want, r.calls)
		}
	}
}
