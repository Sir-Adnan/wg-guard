package install

import (
	"context"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/updatequeue"
)

type nonRootBrokerHost struct{ *memHost }

func (nonRootBrokerHost) IsRoot() bool { return false }

func TestEnsureUpdateBrokerRequiresRootAndOwnedPaths(t *testing.T) {
	nonRoot := nonRootBrokerHost{newMemHost()}
	if err := EnsureUpdateBroker(context.Background(), nonRoot); err == nil {
		t.Fatal("non-root broker install accepted")
	}
	if _, ok := nonRoot.files[UpdateBrokerServicePath]; ok {
		t.Fatal("non-root broker install mutated systemd")
	}

	h := newMemHost()
	h.files[UpdateBrokerServicePath] = memFile{data: []byte("[Service]\nExecStart=/bin/foreign\n"), perm: 0o644}
	if err := EnsureUpdateBroker(context.Background(), h); err == nil || !strings.Contains(err.Error(), "unowned") {
		t.Fatalf("foreign broker unit accepted: %v", err)
	}
	if string(h.files[UpdateBrokerServicePath].data) != "[Service]\nExecStart=/bin/foreign\n" {
		t.Fatal("foreign broker unit was overwritten")
	}
}

func TestEnsureUpdateBrokerRejectsUnownedPendingRequest(t *testing.T) {
	h := newMemHost()
	h.files[updatequeue.New(DataDir).Paths().Request] = memFile{data: []byte(`{"schema":1}`), perm: 0o600}
	if err := EnsureUpdateBroker(context.Background(), h); err == nil || !strings.Contains(err.Error(), "without an owned marker") {
		t.Fatalf("unowned pending request accepted: %v", err)
	}
	if _, ok := h.files[UpdateBrokerServicePath]; ok {
		t.Fatal("broker units written around unowned pending request")
	}
}

func TestEnsureUpdateBrokerRollsBackPartialActivation(t *testing.T) {
	base := newMemHost()
	h := &faultHost{memHost: base, failRun: "enable --now wg-guard-update.path"}
	if err := EnsureUpdateBroker(context.Background(), h); err == nil {
		t.Fatal("partial broker activation accepted")
	}
	for _, path := range []string{UpdateBrokerServicePath, UpdateBrokerPathPath, updatequeue.New(DataDir).Paths().Marker} {
		if _, ok := base.files[path]; ok {
			t.Fatalf("partial broker artifact survived: %s", path)
		}
	}
	if !base.ran("systemctl", "disable", "--now", "wg-guard-update.path") || !base.ran("systemctl", "stop", "wg-guard-update.service") {
		t.Fatalf("partial broker activation was not stopped: %v", base.ranCommands())
	}
}

func TestInspectUpdateBrokerRejectsForeignMarker(t *testing.T) {
	h := newMemHost()
	marker := updatequeue.New(DataDir).Paths().Marker
	h.files[marker] = memFile{data: []byte(`{"schema":2}`), perm: 0o600}
	if _, err := inspectUpdateBroker(h); err == nil || !strings.Contains(err.Error(), "unowned") {
		t.Fatalf("foreign broker marker accepted: %v", err)
	}
}
