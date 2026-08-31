package reconcile

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/iface"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
	"github.com/Sir-Adnan/wg-guard/internal/settings"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel/fake"
)

type harness struct {
	db        *database.DB
	ring      *secrets.KeyRing
	backend   *fake.Backend
	engine    *Engine
	ifaceSvc  *iface.Service
	deviceSeq int
}

func newHarness(t *testing.T, policy Policy) *harness {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "r.db"), database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx, nil); err != nil {
		t.Fatal(err)
	}
	ring, err := secrets.LoadKeyRing(filepath.Join(t.TempDir(), "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	reg, err := settings.New(db, ring, settings.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	ifaceSvc := iface.NewService(db, reg, ring)
	backend := fake.New()
	return &harness{
		db: db, ring: ring, backend: backend, ifaceSvc: ifaceSvc,
		engine: &Engine{DB: db, Backend: backend, Ring: ring, Policy: policy},
	}
}

// seedProfile creates a DB interface + N devices (enabled, active user).
func (h *harness) seedProfile(t *testing.T, name string, devices int, enabled bool) *iface.Interface {
	t.Helper()
	ifc, err := h.ifaceSvc.Create(context.Background(), iface.CreateInput{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	if !enabled {
		if err := h.ifaceSvc.SetEnabled(context.Background(), ifc.ID, false); err != nil {
			t.Fatal(err)
		}
		return ifc
	}
	for i := 0; i < devices; i++ {
		h.seedDevice(t, ifc.ID)
	}
	return ifc
}

func (h *harness) seedDevice(t *testing.T, ifaceID string) string {
	t.Helper()
	h.deviceSeq++
	kp, err := tunnel.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	privEnc, err := h.ring.Encrypt([]byte(kp.Private))
	if err != nil {
		t.Fatal(err)
	}
	psk, _ := tunnel.GeneratePresharedKey()
	pskEnc, err := h.ring.Encrypt([]byte(psk))
	if err != nil {
		t.Fatal(err)
	}
	var uid string
	if err := h.db.QueryRow(`SELECT id FROM users WHERE username='alice'`).Scan(&uid); err != nil {
		if _, e := h.db.Exec(`INSERT INTO users (id, username, status, enabled, created_at, updated_at)
			VALUES (?, 'alice', 'active', 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
			"u-"+kp.Public[:8]); e != nil {
			t.Fatal(e)
		}
		uid = "u-" + kp.Public[:8]
	}
	did := "d-" + kp.Public[:8]
	ipv4 := "10.8.0." + itoa(2+h.deviceSeq) + "/32"
	_, e := h.db.Exec(`INSERT INTO devices (id, user_id, interface_id, name, ipv4_address,
		public_key, private_key_encrypted, preshared_key_encrypted, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		did, uid, ifaceID, "dev-"+did[:8], ipv4, kp.Public, privEnc, pskEnc)
	if e != nil {
		t.Fatal(e)
	}
	return did
}

func TestReconcileFreshBoot(t *testing.T) {
	h := newHarness(t, PolicyReport)
	ctx := context.Background()
	h.seedProfile(t, "awg0", 2, true)

	rep, err := h.engine.Run(ctx)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rep.InterfacesCreated != 1 || rep.PeersAdded != 2 {
		t.Fatalf("report wrong: %+v", rep)
	}
	ops := h.backend.Ops()
	if len(ops) < 3 || ops[0].Kind != fake.OpCreate || ops[1].Kind != fake.OpApply || ops[2].Kind != fake.OpSync {
		t.Fatalf("op sequence wrong: %+v", ops)
	}
	// Second run is a no-op (no pointless syncconf churn).
	h.backend.ResetOps()
	rep, err = h.engine.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rep.InterfacesCreated != 0 || rep.PeersAdded != 0 || len(rep.Drift) != 0 {
		t.Fatalf("steady state must be clean: %+v", rep)
	}
	if len(h.backend.Ops()) != 0 {
		t.Fatalf("steady state must not touch the backend: %+v", h.backend.Ops())
	}
}

func TestReconcileMissingPeerReapplied(t *testing.T) {
	h := newHarness(t, PolicyReport)
	ctx := context.Background()
	h.seedProfile(t, "awg0", 1, true)
	if _, err := h.engine.Run(ctx); err != nil {
		t.Fatal(err)
	}
	h.backend.ResetOps()

	// Simulate drift: the peer vanished from the backend (recreated VPS).
	names, _ := h.backend.ListInterfaces(ctx)
	st, _ := h.backend.Dump(ctx, names[0])
	if len(st.Peers) != 1 {
		t.Fatalf("setup wrong: %d peers", len(st.Peers))
	}
	// Remove the peer via direct backend access: syncconf with an empty list.
	if err := h.backend.SyncPeers(ctx, names[0], nil); err != nil {
		t.Fatal(err)
	}
	h.backend.ResetOps()

	rep, err := h.engine.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rep.PeersAdded != 1 {
		t.Fatalf("missing peer not re-applied: %+v", rep)
	}
	found := false
	for _, d := range rep.Drift {
		if d.Kind == "missing_peer" && d.Action == "added" {
			found = true
		}
	}
	if !found {
		t.Fatalf("drift item missing: %+v", rep.Drift)
	}
}

func TestReconcileUnknownPeerPolicies(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		policy  Policy
		wantAct string
		wantOps int
	}{
		{PolicyReport, "reported", 0},
		{PolicyRemove, "removed", 1},
		{PolicyAdopt, "adopt", 0},
	} {
		t.Run(string(tc.policy), func(t *testing.T) {
			h := newHarness(t, tc.policy)
			h.seedProfile(t, "awg0", 1, true)
			if _, err := h.engine.Run(ctx); err != nil {
				t.Fatal(err)
			}

			// Inject a foreign peer WITHOUT disturbing the desired one
			// (SyncPeers is replace-semantics): pass both.
			st, err := h.backend.Dump(ctx, "awg0")
			if err != nil {
				t.Fatal(err)
			}
			if len(st.Peers) != 1 {
				t.Fatalf("setup: %d peers", len(st.Peers))
			}
			if err := h.backend.SyncPeers(ctx, "awg0", []tunnel.PeerConfig{
				{PublicKey: st.Peers[0].PublicKey, AllowedIPs: st.Peers[0].AllowedIPs},
				{PublicKey: "unknown-peer-key", AllowedIPs: []string{"10.8.0.200/32"}},
			}); err != nil {
				t.Fatal(err)
			}
			h.backend.ResetOps()

			rep, err := h.engine.Run(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var item *DriftItem
			for i := range rep.Drift {
				if rep.Drift[i].Kind == "unknown_peer" {
					item = &rep.Drift[i]
				}
			}
			if item == nil {
				t.Fatalf("unknown peer not reported: %+v", rep.Drift)
			}
			if item.Action != tc.wantAct {
				t.Fatalf("action = %q, want %q", item.Action, tc.wantAct)
			}
			// report/adopt keep the peer (no sync needed); remove syncs.
			if got := len(h.backend.Ops()); got != tc.wantOps {
				t.Fatalf("ops = %d, want %d (%+v)", got, tc.wantOps, h.backend.Ops())
			}
			finalState, _ := h.backend.Dump(ctx, "awg0")
			switch tc.policy {
			case PolicyRemove:
				if len(finalState.Peers) != 1 {
					t.Fatalf("unknown peer not removed: %d peers", len(finalState.Peers))
				}
			default:
				if len(finalState.Peers) != 2 {
					t.Fatalf("unknown peer not kept: %d peers", len(finalState.Peers))
				}
			}
		})
	}
}

func TestReconcileParamDriftCorrected(t *testing.T) {
	h := newHarness(t, PolicyReport)
	ctx := context.Background()
	h.seedProfile(t, "awg0", 1, true)
	if _, err := h.engine.Run(ctx); err != nil {
		t.Fatal(err)
	}
	h.backend.ResetOps()

	// Simulate port drift in the backend (e.g. manual interference).
	names, _ := h.backend.ListInterfaces(ctx)
	_ = names
	// Change the port the backend reports by re-applying with a wrong port.
	st, _ := h.backend.Dump(ctx, "awg0")
	wrong := tunnel.InterfaceConfig{
		PrivateKey: "whatever", ListenPort: st.ListenPort + 1,
		Peers: []tunnel.PeerConfig{{PublicKey: st.Peers[0].PublicKey}},
	}
	if err := h.backend.ApplyInterfaceConfig(ctx, "awg0", wrong); err != nil {
		t.Fatal(err)
	}
	h.backend.ResetOps()

	rep, err := h.engine.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rep.InterfacesUpdated != 1 {
		t.Fatalf("param drift not corrected: %+v", rep)
	}
	st2, _ := h.backend.Dump(ctx, "awg0")
	if st2.ListenPort == st.ListenPort+1 {
		t.Fatal("port still drifted")
	}
	// Peer state must survive the correction (counters kept).
	if len(st2.Peers) != 1 {
		t.Fatalf("peers lost during correction: %+v", st2.Peers)
	}
}

func TestReconcileDisabledInterfaceRemoved(t *testing.T) {
	h := newHarness(t, PolicyReport)
	ctx := context.Background()
	h.seedProfile(t, "awg0", 1, true)
	if _, err := h.engine.Run(ctx); err != nil {
		t.Fatal(err)
	}
	// Disable the profile in the DB.
	ifc, _ := h.ifaceSvc.GetByName(ctx, "awg0")
	if err := h.ifaceSvc.SetEnabled(ctx, ifc.ID, false); err != nil {
		t.Fatal(err)
	}
	rep, err := h.engine.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rep.InterfacesRemoved != 1 {
		t.Fatalf("disabled interface not removed: %+v", rep)
	}
	names, _ := h.backend.ListInterfaces(ctx)
	if len(names) != 0 {
		t.Fatalf("backend still has: %v", names)
	}
}

func TestReconcileForeignInterfaceUntouched(t *testing.T) {
	h := newHarness(t, PolicyReport)
	ctx := context.Background()
	// A foreign interface exists in the backend (e.g. created manually).
	if err := h.backend.CreateInterface(ctx, tunnel.InterfaceSpec{Name: "eth0"}); err != nil {
		t.Fatal(err)
	}
	h.seedProfile(t, "awg0", 1, true)
	rep, err := h.engine.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var foreign bool
	for _, d := range rep.Drift {
		if d.Kind == "foreign_interface" && d.Interface == "eth0" && d.Action == "none" {
			foreign = true
		}
	}
	if !foreign {
		t.Fatalf("foreign interface not reported: %+v", rep.Drift)
	}
	names, _ := h.backend.ListInterfaces(ctx)
	for _, n := range names {
		if n == "eth0" {
			return // still there — untouched
		}
	}
	t.Fatal("foreign interface was removed")
}

func TestReconcileDisabledUserPeerRemoved(t *testing.T) {
	h := newHarness(t, PolicyReport)
	ctx := context.Background()
	h.seedProfile(t, "awg0", 2, true)
	if _, err := h.engine.Run(ctx); err != nil {
		t.Fatal(err)
	}
	// Disable one device → its peer must disappear from the backend.
	var devID string
	if err := h.db.QueryRow(`SELECT id FROM devices LIMIT 1`).Scan(&devID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.Exec(`UPDATE devices SET enabled = 0 WHERE id = ?`, devID); err != nil {
		t.Fatal(err)
	}
	h.backend.ResetOps()
	rep, err := h.engine.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rep.PeersRemoved != 1 {
		t.Fatalf("stale peer not removed: %+v", rep)
	}
	st, _ := h.backend.Dump(ctx, "awg0")
	if len(st.Peers) != 1 {
		t.Fatalf("backend peers = %d, want 1", len(st.Peers))
	}
}

func TestReconcileFailureInjected(t *testing.T) {
	h := newHarness(t, PolicyReport)
	ctx := context.Background()
	h.seedProfile(t, "awg0", 1, true)
	boom := errors.New("injected backend failure")
	h.backend.FailOn[fake.OpSync] = boom
	rep, err := h.engine.Run(ctx)
	if err != nil {
		t.Fatalf("per-interface failures are collected, not returned: %v", err)
	}
	if len(rep.Errors) != 1 || !strings.Contains(rep.Errors[0].Err, "injected backend failure") {
		t.Fatalf("errors = %+v", rep.Errors)
	}
	if rep.Errors[0].Interface != "awg0" {
		t.Fatalf("error interface = %q", rep.Errors[0].Interface)
	}
}

// A failing interface must not abort the pass: interfaces are processed in
// name order, so awg0's (one-shot) sync failure is collected and awg1 still
// reconciles cleanly.
func TestReconcileFailureDoesNotAbortOtherInterfaces(t *testing.T) {
	h := newHarness(t, PolicyReport)
	ctx := context.Background()
	h.seedProfile(t, "awg0", 1, true)
	h.seedProfile(t, "awg1", 1, true)
	h.backend.FailOn[fake.OpSync] = errors.New("awg0-only failure")

	rep, err := h.engine.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Errors) != 1 || rep.Errors[0].Interface != "awg0" {
		t.Fatalf("errors = %+v", rep.Errors)
	}
	// awg1 was fully created despite awg0's failure.
	st, err := h.backend.Dump(ctx, "awg1")
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Peers) != 1 {
		t.Fatalf("awg1 peers = %d, want 1", len(st.Peers))
	}
}

// An obfuscation-mode transition (foreign drift, or a future profile-edit
// flow) cannot be done with setconf on the pinned runtime — the engine
// recreates the link and re-syncs peers.
func TestReconcileModeTransitionRecreates(t *testing.T) {
	h := newHarness(t, PolicyReport)
	ctx := context.Background()
	h.seedProfile(t, "awg0", 1, true) // plain profile
	if _, err := h.engine.Run(ctx); err != nil {
		t.Fatal(err)
	}
	h.backend.ResetOps()

	// Foreign drift: someone obfuscated the interface out-of-band.
	if err := h.backend.SetObfuscation("awg0", tunnel.Obfuscation{
		Enabled: true, Jc: 8, Jmin: 40, Jmax: 70, S1: 15, S2: 64,
		H1: 1, H2: 2, H3: 3, H4: 4,
	}); err != nil {
		t.Fatal(err)
	}

	rep, err := h.engine.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var item *DriftItem
	for i := range rep.Drift {
		if rep.Drift[i].Kind == "mode_transition" {
			item = &rep.Drift[i]
		}
	}
	if item == nil || item.Action != "recreated" {
		t.Fatalf("mode transition not recreated: %+v", rep.Drift)
	}
	ops := h.backend.Ops()
	if len(ops) < 2 || ops[0].Kind != fake.OpRemove || ops[1].Kind != fake.OpCreate {
		t.Fatalf("op sequence must be remove+create: %+v", ops)
	}
	if rep.PeersAdded != 1 {
		t.Fatalf("peers must be re-synced after recreate: %+v", rep)
	}
	// The interface is plain again after recreation.
	st, err := h.backend.Dump(ctx, "awg0")
	if err != nil {
		t.Fatal(err)
	}
	if st.Obfuscation.Enabled || len(st.Peers) != 1 {
		t.Fatalf("post-recreate state = %+v peers=%d", st.Obfuscation, len(st.Peers))
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func TestReconcilePskPreservedForUnknownReportedPeer(t *testing.T) {
	h := newHarness(t, PolicyReport)
	ctx := context.Background()
	h.seedProfile(t, "awg0", 0, true)
	if _, err := h.engine.Run(ctx); err != nil {
		t.Fatal(err)
	}
	// Unknown peer WITH a preshared key (set directly in the backend).
	if err := h.backend.SyncPeers(ctx, "awg0", []tunnel.PeerConfig{
		{PublicKey: "ghost", AllowedIPs: []string{"10.8.0.200/32"}, PresharedKey: "its-psk"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.engine.Run(ctx); err != nil {
		t.Fatal(err)
	}
	st, _ := h.backend.Dump(ctx, "awg0")
	for _, p := range st.Peers {
		if p.PublicKey == "ghost" && !p.PresharedKeySet {
			t.Fatal("reported peer lost its preshared key")
		}
	}
}

// TestEngineCreateCarriesPrivateKey is the regression for the VPS finding
// (2026-08-31): the creation spec must carry the decrypted private key —
// the backend renders the initial setconf from the spec, and the pinned
// tooling rejects an empty PrivateKey line (userspace tolerated it, the
// kernel module does not; the fake never checked).
func TestEngineCreateCarriesPrivateKey(t *testing.T) {
	h := newHarness(t, PolicyReport)
	ctx := context.Background()
	ifc := h.seedProfile(t, "awg0", 0, true)

	if _, err := h.engine.Run(ctx); err != nil {
		t.Fatal(err)
	}
	spec, ok := h.backend.Spec("awg0")
	if !ok {
		t.Fatal("interface never created")
	}
	if spec.PrivateKey == "" {
		t.Fatal("creation spec has an empty private key")
	}
	want, err := h.ifaceSvc.PrivateKey(ifc)
	if err != nil {
		t.Fatal(err)
	}
	if spec.PrivateKey != want {
		t.Fatal("creation spec carries a different key than the stored one")
	}
}
