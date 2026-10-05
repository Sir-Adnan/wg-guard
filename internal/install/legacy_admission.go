package install

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Legacy paths are read-only collision checks, never managed execution targets.
const legacyServerUnitPath = "/etc/systemd/system/wg-guard.service"
const legacyInstallStatePath = "/etc/wg-guard/install-state.json"

// systemd may retain an active unit after its file disappears. An unowned legacy
// server cannot be stopped or silently adopted by a fresh Docker installation.
func checkLegacyServerAbsent(ctx context.Context, h Host) error {
	raw, err := h.Output(ctx, []string{"systemctl", "show", "wg-guard.service", "--property=LoadState", "--property=ActiveState"}, 30*time.Second)
	if err != nil || len(raw) > 1024 {
		return fmt.Errorf("install: cannot rule out an existing host server; inspect wg-guard.service before installing")
	}
	values := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || values[key] != "" || (key != "LoadState" && key != "ActiveState") {
			return fmt.Errorf("install: existing host server state is indeterminate")
		}
		values[key] = value
	}
	if len(values) != 2 || values["LoadState"] != "not-found" || values["ActiveState"] != "inactive" {
		return fmt.Errorf("install: an existing host server must be backed up and removed with its original manager before a fresh Docker installation")
	}
	return nil
}

func checkFreshContainerAbsent(ctx context.Context, h Host) error {
	if _, err := h.LookPath("docker"); err != nil {
		return nil
	}
	raw, err := h.Output(ctx, []string{"docker", "ps", "-a", "--filter", "name=^/" + Container + "$", "--format", "{{.ID}}"}, 30*time.Second)
	if err != nil || len(raw) > 4096 {
		return fmt.Errorf("install: cannot inspect the existing Docker engine; recover it before installing")
	}
	if strings.TrimSpace(raw) != "" {
		return fmt.Errorf("install: an existing wg-guard container is not owned by this fresh installation; back up and remove it with its original manager")
	}
	return nil
}
