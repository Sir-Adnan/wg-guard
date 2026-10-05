package install

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/updatequeue"
)

type maintenanceHost struct {
	*memHost
	free string
}

func (h *maintenanceHost) Output(ctx context.Context, argv []string, d time.Duration) (string, error) {
	if len(argv) > 0 && argv[0] == "df" {
		return "Avail\n" + h.free + "\n", nil
	}
	return h.memHost.Output(ctx, argv, d)
}

func TestPreflightIsReadOnlyAndBlocksLowDisk(t *testing.T) {
	h := &maintenanceHost{memHost: installedFixture(t, ModeDocker), free: "1024"}
	before := string(h.files[StatePath].data)
	h.commands = nil
	i, err := MaintenancePreflight(context.Background(), h, updatequeue.Input{Operation: updatequeue.OperationPanel, Channel: "release", Ref: "v0.1.7"}, nil)
	if err != nil || i.Preflight == nil || i.Preflight.Ready {
		t.Fatal("low-disk maintenance was ready", err)
	}
	if string(h.files[StatePath].data) != before || h.ran("docker", "build") || h.ran("systemctl", "restart") {
		t.Fatal("preview mutated deployment")
	}
	for _, cmd := range h.commands {
		if len(cmd.argv) > 1 && cmd.argv[0] == "apt-get" {
			t.Fatal("preview installed packages")
		}
	}
}

func TestManagedRuntimeIncludesExactUserspaceProvenance(t *testing.T) {
	b, _ := SelectCore("recommended")
	dockerfile := RuntimeDockerfile(b)
	for _, value := range []string{b.UserspaceVersion, b.UserspaceCommit, "/usr/local/bin/amneziawg-go", "vcs.modified=false", "CGO_ENABLED=0"} {
		if !strings.Contains(dockerfile, value) {
			t.Fatalf("managed image missing %s", value)
		}
	}
	// Arbitrary bundle identities remain rejected by the actual image builder.

}
