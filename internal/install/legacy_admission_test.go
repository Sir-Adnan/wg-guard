package install

import (
	"context"
	"encoding/json"
	"io"
	"testing"
)

func TestLegacyDeploymentStateRefusesMutation(t *testing.T) {
	for _, mode := range []string{"native", "docker"} {
		for _, schema := range []int{1, 2, 3} {
			h := newMemHost()
			raw, _ := json.Marshal(map[string]any{"schema": schema, "mode": mode, "config_path": ConfigPath, "data_dir": DataDir, "compose_path": "/etc/wg-guard/compose.yaml"})
			h.files[StatePath] = memFile{data: raw, perm: 0600}
			_, err := Install(context.Background(), h, InstallOptions{Plan: Defaults(), Yes: true, Stdout: io.Discard})
			if err == nil || h.ran("modprobe") || h.ran("apt-get") || h.ran("docker", "compose") || h.ran("systemctl", "stop") {
				t.Fatal("legacy deployment was executed", mode, schema, err)
			}
			if string(h.files[StatePath].data) != string(raw) {
				t.Fatal("legacy state changed during refusal")
			}
		}
	}
}

func TestLegacyStateInOriginalDirectoryIsNotMistakenForFreshInstall(t *testing.T) {
	h := newMemHost()
	h.files[legacyInstallStatePath] = memFile{data: []byte(`{"schema":3,"mode":"docker"}`), perm: 0600}
	if err := CheckLifecycleReady(h); err == nil {
		t.Fatal("old directory bypassed admission")
	}
	if _, err := LoadState(h); err == nil {
		t.Fatal("old directory treated as not installed")
	}
}

func TestLoadedOrIndeterminateLegacyUnitBlocksFreshInstall(t *testing.T) {
	for _, raw := range []string{"LoadState=loaded\nActiveState=active\n", "LoadState=loaded\nActiveState=inactive\n", "LoadState=not-found\nActiveState=active\n", "LoadState=not-found\n", "", "LoadState=not-found\nActiveState=inactive\nActiveState=active\n"} {
		h := newMemHost()
		h.output["systemctl show wg-guard.service --property=LoadState --property=ActiveState"] = raw
		_, err := Install(context.Background(), h, InstallOptions{Plan: Defaults(), Yes: true, Stdout: io.Discard})
		if err == nil {
			t.Fatal("unowned legacy unit accepted")
		}
		if h.ran("modprobe") || h.ran("systemctl", "stop") || h.ran("docker", "compose", "-f") {
			t.Fatal("legacy unit was mutated")
		}
		if _, exists := h.files[ConfigPath]; exists {
			t.Fatal("deployment created before legacy admission")
		}
	}
}

func TestDockerStateRefusesRetiredFieldsAndTrailingRecords(t *testing.T) {
	h := installedFixture(t, ModeDocker)
	raw := string(h.files[StatePath].data)
	for _, bad := range []string{raw + " {}", `{"schema":4,"mode":"docker","unit_path":"/etc/systemd/system/wg-guard.service"}`} {
		h.files[StatePath] = memFile{data: []byte(bad), perm: 0600}
		if _, err := LoadState(h); err == nil {
			t.Fatal("ambiguous/retired state accepted")
		}
	}
}
