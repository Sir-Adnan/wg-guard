package routing

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/egress"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
	"github.com/Sir-Adnan/wg-guard/internal/subprocess"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel/fake"
)

// hostRunner models nft and iproute2 state just enough for Manager.
type hostRunner struct {
	mu      sync.Mutex
	table   bool
	sets    map[string]bool // sets the live table holds
	scripts []string
	rules   map[string]bool // "mark table"
	argv    [][]string
}

func (h *hostRunner) Run(_ context.Context, argv []string) (subprocess.Result, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.argv = append(h.argv, argv)
	join := strings.Join(argv, " ")
	switch {
	case strings.HasPrefix(join, "nft -t -j list table"):
		if !h.table {
			return subprocess.Result{}, &subprocess.ExitError{Name: "nft", ExitCode: 1, Stderr: "Error: No such file or directory"}
		}
		objs := []string{`{"chain":{"family":"ip","table":"` + TableName + `","name":"prerouting"}}`}
		for name := range h.sets {
			objs = append(objs, `{"set":{"family":"ip","table":"`+TableName+`","name":"`+name+`"}}`)
		}
		return subprocess.Result{Stdout: []byte(`{"nftables":[` + strings.Join(objs, ",") + `]}`)}, nil
	case strings.HasPrefix(join, "nft -f "):
		data, _ := os.ReadFile(argv[2])
		h.scripts = append(h.scripts, string(data))
		h.table = true
		if h.sets == nil {
			h.sets = map[string]bool{}
		}
		for _, l := range strings.Split(string(data), "\n") {
			if f := strings.Fields(l); len(f) > 4 && f[0] == "add" && f[1] == "set" {
				h.sets[f[4]] = true
			}
		}
	case strings.HasPrefix(join, "nft delete table"):
		h.table, h.sets = false, nil
	case join == "ip -j -4 rule show":
		var parts []string
		for r := range h.rules {
			f := strings.Fields(r)
			parts = append(parts, `{"priority":5180,"fwmark":"`+f[0]+`","fwmask":"0x7ff00000","table":"`+f[1]+`"}`)
		}
		return subprocess.Result{Stdout: []byte("[" + strings.Join(parts, ",") + "]")}, nil
	case strings.HasPrefix(join, "ip -4 rule add"):
		h.rules[argv[7][:strings.Index(argv[7], "/")]+" "+argv[9]] = true
	case strings.HasPrefix(join, "ip -4 rule del"):
		delete(h.rules, argv[7][:strings.Index(argv[7], "/")]+" "+argv[9])
	}
	return subprocess.Result{}, nil
}

func (h *hostRunner) lastScript() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.scripts) == 0 {
		return ""
	}
	return h.scripts[len(h.scripts)-1]
}

type fixedProber struct {
	mu  sync.Mutex
	ok  map[uint32]bool
	got []uint32
}

func (p *fixedProber) Probe(_ context.Context, mark uint32) ProbeResult {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.got = append(p.got, mark)
	return ProbeResult{OK: p.ok[mark], ExitIP: "104.28.0.1", Country: "DE"}
}

type nullResolver struct {
	wanted    bool
	forgotten []string
}

func (r *nullResolver) Configure(_ context.Context, cfg ResolverConfig) bool {
	r.wanted = !cfg.Ruleset.Empty()
	return r.wanted
}

func (r *nullResolver) Forget(sets []string) { r.forgotten = append(r.forgotten, sets...) }

func TestManagerAppliesAndFailsOverOnHealth(t *testing.T) {
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "e.db"), database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.Migrate(ctx, nil); err != nil {
		t.Fatal(err)
	}
	ring, err := secrets.LoadKeyRing(filepath.Join(dir, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	store := egress.NewService(db, ring)
	conf := "[Interface]\nPrivateKey = " + keyA + "\nAddress = 10.7.0.2/32\n[Peer]\nPublicKey = " + keyB + "\nEndpoint = 198.51.100.4:51820\n"
	nl, err := store.CreateWireGuard(ctx, "NL", conf)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateRule(ctx, egress.RuleInput{Name: "AI", Enabled: true,
		Target: egress.Target{Kind: egress.TargetOutbound, ID: nl.ID}, Presets: []string{"gemini"}}); err != nil {
		t.Fatal(err)
	}
	host := &hostRunner{rules: map[string]bool{"0x500000 51805": true}} // stale rule from a deleted slot
	backend := fake.New()
	prober := &fixedProber{ok: map[uint32]bool{SlotMark(nl.Slot): true}}
	resolver := &nullResolver{}
	m := &Manager{Store: store, Backend: backend, Run: host, Resolver: resolver, Prober: prober}
	if err := m.Apply(ctx, []Interface{{Name: "awg0", Pools: []string{"10.8.0.0/24"}}}); err != nil {
		t.Fatal(err)
	}
	if st, err := backend.Dump(ctx, "wgo1"); err != nil || len(st.Peers) != 1 || st.Peers[0].PublicKey != keyB {
		t.Fatalf("outbound link: %+v %v", st, err)
	}
	if !host.rules["0x100000 51801"] || host.rules["0x500000 51805"] {
		t.Fatalf("policy rules: %v", host.rules)
	}
	script := host.lastScript()
	if !strings.Contains(script, "goto t_o1") || !strings.Contains(script, "redirect to :53") || !resolver.wanted {
		t.Fatalf("table:\n%s", script)
	}
	st := m.Status()
	if o := st.Outbounds[nl.ID]; !o.LinkUp || o.Link != "wgo1" || !o.Healthy || !st.ResolverActive {
		t.Fatalf("status: %+v", st)
	}

	// Unchanged state reuses the link (no recreate).
	backend.ResetOps()
	if err := m.Reapply(ctx); err != nil {
		t.Fatal(err)
	}
	for _, op := range backend.Ops() {
		if op.Kind == "create" || op.Kind == "remove" {
			t.Fatalf("unchanged link was recreated: %v", backend.Ops())
		}
	}

	// Two failed probes take the outbound down and the rule fails open.
	prober.ok[SlotMark(nl.Slot)] = false
	for i := 0; i < 2; i++ {
		if err := m.ProbeAll(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if script := host.lastScript(); strings.Contains(script, "goto t_o1") || !strings.Contains(script, "goto t_direct") {
		t.Fatalf("down outbound still routed:\n%s", script)
	}
	if o := m.Status().Outbounds[nl.ID]; o.Healthy || o.Failures != 2 || o.Probe.Country != "DE" {
		t.Fatalf("health status: %+v", o)
	}
	// One success brings it back. The panel Test only records it (no kernel
	// change from a read-scope action); the next health pass renders it.
	prober.ok[SlotMark(nl.Slot)] = true
	scripts := len(host.scripts)
	if _, err := m.Test(ctx, nl.ID); err != nil {
		t.Fatal(err)
	}
	if len(host.scripts) != scripts || !m.Status().Outbounds[nl.ID].Healthy {
		t.Fatalf("Test must record health without re-rendering: scripts %d→%d", scripts, len(host.scripts))
	}
	if err := m.ProbeAll(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(host.lastScript(), "goto t_o1") {
		t.Fatal("recovered outbound not restored by the health pass")
	}

	// Removing everything removes the link, rules and table, then goes idle.
	rules, _ := store.ListRules(ctx)
	for _, r := range rules {
		_ = store.DeleteRule(ctx, r.ID)
	}
	if err := store.DeleteOutbound(ctx, nl.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.Reapply(ctx); err != nil {
		t.Fatal(err)
	}
	if names, _ := backend.ListInterfaces(ctx); len(names) != 0 || len(host.rules) != 0 || host.table {
		t.Fatalf("leftovers: links=%v rules=%v table=%v", names, host.rules, host.table)
	}
	if len(m.health) != 0 {
		t.Fatalf("health of a deleted outbound kept: %v", m.health)
	}
	host.argv = nil
	if err := m.Reapply(ctx); err != nil || len(host.argv) != 0 {
		t.Fatalf("idle pass ran commands: %v %v", host.argv, err)
	}
}

func TestManagerSkipsRouteForFailedLink(t *testing.T) {
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "e.db"), database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.Migrate(ctx, nil); err != nil {
		t.Fatal(err)
	}
	ring, err := secrets.LoadKeyRing(filepath.Join(dir, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	store := egress.NewService(db, ring)
	conf := "[Interface]\nPrivateKey = " + keyA + "\nAddress = 10.7.0.2/32\n[Peer]\nPublicKey = " + keyB + "\nEndpoint = 198.51.100.4:51820\n"
	nl, err := store.CreateWireGuard(ctx, "NL", conf)
	if err != nil {
		t.Fatal(err)
	}
	host := &hostRunner{rules: map[string]bool{}}
	backend := fake.New()
	backend.FailOn[fake.OpApply] = errors.New("apply refused")
	m := &Manager{Store: store, Backend: backend, Run: host, Resolver: &nullResolver{}, Prober: &fixedProber{ok: map[uint32]bool{}}}
	if err := m.Apply(ctx, []Interface{{Name: "awg0", Pools: []string{"10.8.0.0/24"}}}); err != nil {
		t.Fatalf("a failed link must not fail the pass: %v", err)
	}
	for _, argv := range host.argv {
		if strings.HasPrefix(strings.Join(argv, " "), "ip -4 route replace") {
			t.Fatalf("route installed for a failed link: %v", argv)
		}
	}
	if len(host.rules) != 0 {
		t.Fatalf("policy rule for a failed link: %v", host.rules)
	}
	if o := m.Status().Outbounds[nl.ID]; o.LinkError != "create" || o.Healthy {
		t.Fatalf("status: %+v", o)
	}
	// The next pass retries and the link comes up.
	if err := m.Reapply(ctx); err != nil {
		t.Fatal(err)
	}
	if !host.rules["0x100000 51801"] {
		t.Fatalf("retry did not install the rule: %v", host.rules)
	}
}

func newTestStore(t *testing.T) (*egress.Service, func()) {
	t.Helper()
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "e.db"), database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	ring, err := secrets.LoadKeyRing(filepath.Join(dir, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	return egress.NewService(db, ring), func() { db.Close() }
}

func TestManagerForgetsResolverMemoryOfRecreatedSets(t *testing.T) {
	store, done := newTestStore(t)
	defer done()
	ctx := context.Background()
	rule, err := store.CreateRule(ctx, egress.RuleInput{Name: "AI", Enabled: true, Target: egress.Direct, Matchers: "gemini.google.com"})
	if err != nil {
		t.Fatal(err)
	}
	set := DynamicSetName(rule.ID)
	host := &hostRunner{rules: map[string]bool{}}
	resolver := &nullResolver{}
	m := &Manager{Store: store, Backend: fake.New(), Run: host, Resolver: resolver, Prober: &fixedProber{ok: map[uint32]bool{}}}
	if err := m.Apply(ctx, []Interface{{Name: "awg0", Pools: []string{"10.8.0.0/24"}}}); err != nil {
		t.Fatal(err)
	}
	if len(resolver.forgotten) != 1 || resolver.forgotten[0] != set {
		t.Fatalf("new set not forgotten: %v", resolver.forgotten)
	}
	if err := m.Reapply(ctx); err != nil || len(resolver.forgotten) != 1 {
		t.Fatalf("a surviving set must keep its memory: %v %v", resolver.forgotten, err)
	}
	// Something outside WG-Guard flushes the ruleset; the health pass
	// rebuilds the table and the resolver must re-learn into the empty set.
	host.mu.Lock()
	host.table, host.sets = false, nil
	host.mu.Unlock()
	if err := m.ProbeAll(ctx); err != nil {
		t.Fatal(err)
	}
	if len(resolver.forgotten) != 2 || resolver.forgotten[1] != set {
		t.Fatalf("rebuilt set not forgotten: %v", resolver.forgotten)
	}
}

func TestManagerAdoptConvergesSurvivingLinkInPlace(t *testing.T) {
	store, done := newTestStore(t)
	defer done()
	ctx := context.Background()
	conf := "[Interface]\nPrivateKey = " + keyA + "\nAddress = 10.7.0.2/32\nMTU = 1380\n[Peer]\nPublicKey = " + keyB + "\nEndpoint = 198.51.100.4:51820\n"
	nl, err := store.CreateWireGuard(ctx, "NL", conf)
	if err != nil {
		t.Fatal(err)
	}
	backend := fake.New()
	tunnels := []Interface{{Name: "awg0", Pools: []string{"10.8.0.0/24"}}}
	first := &Manager{Store: store, Backend: backend, Run: &hostRunner{rules: map[string]bool{}}}
	if err := first.Apply(ctx, tunnels); err != nil {
		t.Fatal(err)
	}
	if err := backend.SetPublicKey("wgo1", tunnelPublicKey(t, keyA)); err != nil {
		t.Fatal(err)
	}
	// Committed but not applied before a restart: endpoint, keepalive off
	// and MTU change; identity (keys, obfuscation) is unchanged.
	changed := strings.Replace(conf, "198.51.100.4:51820", "198.51.100.9:51821", 1)
	changed = strings.Replace(changed, "MTU = 1380", "MTU = 1300", 1) + "PersistentKeepalive = 0\n"
	if _, err := store.ReplaceWireGuardConfig(ctx, nl.ID, changed); err != nil {
		t.Fatal(err)
	}
	backend.ResetOps()
	host := &hostRunner{rules: map[string]bool{}}
	restarted := &Manager{Store: store, Backend: backend, Run: host}
	if err := restarted.Apply(ctx, tunnels); err != nil {
		t.Fatal(err)
	}
	synced := false
	for _, op := range backend.Ops() {
		if op.Kind == fake.OpCreate || op.Kind == fake.OpRemove {
			t.Fatalf("matching identity must be adopted, not recreated: %v", backend.Ops())
		}
		synced = synced || op.Kind == fake.OpSync
	}
	st, err := backend.Dump(ctx, "wgo1")
	if err != nil || !synced || len(st.Peers) != 1 || st.Peers[0].Endpoint != "198.51.100.9:51821" || !st.Peers[0].PersistentKeepalive.IsZero() {
		t.Fatalf("adopted link not converged: synced=%v %+v %v", synced, st, err)
	}
	mtuSet := false
	for _, argv := range host.argv {
		mtuSet = mtuSet || strings.Join(argv, " ") == "ip link set dev wgo1 mtu 1300"
	}
	if !mtuSet {
		t.Fatalf("MTU not converged in place: %v", host.argv)
	}

	// A different identity is recreated.
	other := strings.Replace(changed, "PublicKey = "+keyB, "PublicKey = "+tunnelPublicKey(t, keyA), 1)
	if _, err := store.ReplaceWireGuardConfig(ctx, nl.ID, other); err != nil {
		t.Fatal(err)
	}
	backend.ResetOps()
	again := &Manager{Store: store, Backend: backend, Run: &hostRunner{rules: map[string]bool{}}}
	if err := again.Apply(ctx, tunnels); err != nil {
		t.Fatal(err)
	}
	recreated := false
	for _, op := range backend.Ops() {
		recreated = recreated || op.Kind == fake.OpCreate
	}
	if !recreated {
		t.Fatalf("changed peer identity must recreate the link: %v", backend.Ops())
	}
}

func tunnelPublicKey(t *testing.T, priv string) string {
	t.Helper()
	pub, err := tunnel.PublicKeyFromPrivate(priv)
	if err != nil {
		t.Fatal(err)
	}
	return pub
}
