package install

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/distribution"
	"github.com/Sir-Adnan/wg-guard/internal/updatequeue"
)

func TestPreparedCandidateDoesNotChangeManagerOrDeployment(t *testing.T) {
	h := newMemHost()
	bytes := []byte("prepared release")
	h.files["/tmp/candidate"] = memFile{data: bytes, perm: 0o755}
	h.files[ManagerBinaryPath] = memFile{data: []byte("active manager"), perm: 0o755}
	h.files[BinPath] = memFile{data: []byte("running panel"), perm: 0o755}
	b := distribution.Build{Channel: "release", Ref: "v1.2.3", Version: "v1.2.3", Commit: strings.Repeat("a", 40), SHA256: fmt.Sprintf("%x", sha256.Sum256(bytes)), BinaryPath: "/tmp/candidate"}
	if err := CacheMaintenanceBuild(context.Background(), h, b); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadPreparedMaintenanceBuild(context.Background(), h)
	if err != nil || loaded.Commit != b.Commit || loaded.SHA256 != b.SHA256 {
		t.Fatal("prepared identity lost", err)
	}
	if string(h.files[ManagerBinaryPath].data) != "active manager" || string(h.files[BinPath].data) != "running panel" || h.ran("systemctl", "restart") {
		t.Fatal("preparing changed the running deployment")
	}
	h.files[preparedBinary] = memFile{data: []byte("tampered"), perm: 0o755}
	if _, err := LoadPreparedMaintenanceBuild(context.Background(), h); err == nil {
		t.Fatal("tampered prepared binary accepted")
	}
}

func TestBridgeCapabilitiesFollowInstalledArtifact(t *testing.T) {
	h := newMemHost()
	if err := EnsureUpdateBrokerForContract(context.Background(), h, CurrentContract()); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.files[UpdateBrokerTimerPath]; !ok {
		t.Fatal("enhanced timer not installed")
	}
	legacy := CurrentContract()
	legacy.MaintenanceProtocol = 0
	if err := EnsureUpdateBrokerForContract(context.Background(), h, legacy); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.files[UpdateBrokerTimerPath]; ok {
		t.Fatal("old artifact retained an unsupported timer")
	}
	if string(h.files[updatequeue.New(DataDir).Paths().Marker].data) != "{\"schema\":1}\n" {
		t.Fatal("old artifact advertised new capabilities")
	}
	if !h.ran("systemctl", "disable", "--now", "wg-guard-update.timer") {
		t.Fatal("legacy timer was not disabled")
	}
	if err := EnsureUpdateBrokerForContract(context.Background(), h, CurrentContract()); err != nil {
		t.Fatal("forward repair failed", err)
	}
}
