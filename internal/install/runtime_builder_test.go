package install

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// builderHost answers the BuildKit probe from its simulated package database.
type builderHost struct {
	*packageHost
	installFails bool
}

func newBuilderHost() *builderHost {
	h := &builderHost{packageHost: newPackageHost()}
	h.installed["docker.io"] = "29.1.3-0ubuntu3~24.04.2"
	h.available[RuntimeBuilderPackage] = "0.30.1-0ubuntu1~24.04.1"
	return h
}

func (h *builderHost) Output(ctx context.Context, a []string, d time.Duration) (string, error) {
	if len(a) > 2 && a[0] == "docker" && a[1] == "buildx" {
		if h.installed[RuntimeBuilderPackage] == "" {
			return "", fmt.Errorf("unknown command: docker buildx")
		}
		return "github.com/docker/buildx 0.30.1", nil
	}
	return h.packageHost.Output(ctx, a, d)
}

func (h *builderHost) Run(ctx context.Context, a []string, d time.Duration) error {
	if h.installFails && len(a) > 1 && a[0] == "apt-get" && a[1] == "install" {
		return fmt.Errorf("dpkg interrupted")
	}
	return h.packageHost.Run(ctx, a, d)
}

func ensureBuilder(t *testing.T, h *builderHost, policy PrerequisitePolicy) (*State, string) {
	t.Helper()
	platform, _ := InspectPlatform(context.Background(), h)
	st := &State{Schema: StateSchema, Mode: ModeDocker, ConfigPath: ConfigPath, DataDir: DataDir, BinPath: BinPath, ComposePath: ComposePth}
	var out bytes.Buffer
	EnsureRuntimeBuilder(context.Background(), h, platform, policy, st, &out)
	return st, out.String()
}

func TestRuntimeBuilderInstallsAndRecordsBuildKitBesideUbuntuDocker(t *testing.T) {
	h := newBuilderHost()
	st, out := ensureBuilder(t, h, PrerequisitesAuto)
	if !contains(h.installedArgs, RuntimeBuilderPackage) || !contains(st.PackagesInstalled, RuntimeBuilderPackage) {
		t.Fatalf("BuildKit was not installed and recorded: args=%v packages=%v", h.installedArgs, st.PackagesInstalled)
	}
	for _, command := range h.commands {
		if len(command.argv) > 1 && command.argv[0] == "apt-get" && command.argv[1] == "install" &&
			(!contains(command.argv, "--no-remove") || !contains(command.argv, "--no-install-recommends")) {
			t.Fatalf("builder install may remove or add unrelated packages: %v", command.argv)
		}
	}
	if strings.Contains(out, "legacy") {
		t.Fatalf("successful provisioning warned about the legacy builder: %q", out)
	}
	if err := validateState(st); err != nil {
		t.Fatalf("installer-owned BuildKit was rejected by state validation: %v", err)
	}
}

func TestRuntimeBuilderLeavesExistingBuildKitUntouched(t *testing.T) {
	h := newBuilderHost()
	h.installed[RuntimeBuilderPackage] = "system"
	st, out := ensureBuilder(t, h, PrerequisitesAuto)
	if h.ran("apt-get") || len(st.PackagesInstalled) != 0 || out != "" {
		t.Fatalf("available BuildKit was changed: ran=%v packages=%v out=%q", h.ranCommands(), st.PackagesInstalled, out)
	}
}

func TestRuntimeBuilderFallsBackWithoutMutatingForeignOrManualHosts(t *testing.T) {
	for name, setup := range map[string]func(*builderHost) PrerequisitePolicy{
		"foreign engine": func(h *builderHost) PrerequisitePolicy { delete(h.installed, "docker.io"); return PrerequisitesAuto },
		"check policy":   func(*builderHost) PrerequisitePolicy { return PrerequisitesCheck },
		"unavailable": func(h *builderHost) PrerequisitePolicy {
			delete(h.available, RuntimeBuilderPackage)
			return PrerequisitesAuto
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newBuilderHost()
			st, out := ensureBuilder(t, h, setup(h))
			if h.ran("apt-get") || len(st.PackagesInstalled) != 0 {
				t.Fatalf("builder fallback mutated the host: ran=%v packages=%v", h.ranCommands(), st.PackagesInstalled)
			}
			if !strings.Contains(out, "legacy builder") {
				t.Fatalf("fallback did not explain the slower legacy builder: %q", out)
			}
		})
	}
}

func TestRuntimeBuilderInstallFailureIsNotFatal(t *testing.T) {
	h := newBuilderHost()
	h.installFails = true
	st, out := ensureBuilder(t, h, PrerequisitesAuto)
	if contains(st.PackagesInstalled, RuntimeBuilderPackage) || !strings.Contains(out, "legacy builder") {
		t.Fatalf("failed builder install was recorded or not reported: packages=%v out=%q", st.PackagesInstalled, out)
	}
}
