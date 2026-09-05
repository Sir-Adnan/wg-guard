package install

import (
	"context"
	"io"
	"strings"
	"time"
)

type PrerequisitePolicy string

const (
	PrerequisitesAuto  PrerequisitePolicy = "auto"
	PrerequisitesCheck PrerequisitePolicy = "check"
)

// CoreBundle identifies a reviewed compatibility contract, not an upstream
// version range. Adding a newer bundle requires upstream and runtime evidence.
type CoreBundle struct {
	ID               string `json:"id"`
	ToolsVersion     string `json:"tools_version"`
	ToolsCommit      string `json:"tools_commit"`
	ToolsPackage     string `json:"tools_package"`
	KernelVersion    string `json:"kernel_version"`
	KernelCommit     string `json:"kernel_commit"`
	KernelPackage    string `json:"kernel_package"`
	UserspaceVersion string `json:"userspace_version"`
	UserspaceCommit  string `json:"userspace_commit"`
}

type CoreReport struct {
	Requested      CoreBundle `json:"requested"`
	ToolsPackage   string     `json:"installed_tools_package,omitempty"`
	KernelPackage  string     `json:"installed_kernel_package,omitempty"`
	ToolsVersion   string     `json:"observed_tools_version,omitempty"`
	LoadedVersion  string     `json:"loaded_module_version,omitempty"`
	LoadedSource   string     `json:"loaded_module_srcversion,omitempty"`
	DiskSource     string     `json:"disk_module_srcversion,omitempty"`
	ModuleLoaded   bool       `json:"module_loaded"`
	RebootRequired bool       `json:"reboot_required"`
	ModuleIdentity string     `json:"module_identity"` // unknown, matches-disk, differs-from-disk
	ExternalModule bool       `json:"external_module"`
	ToolsLocation  string     `json:"tools_location"`
}

func SelectCore(selector string) (CoreBundle, error) {
	switch selector {
	case "", "recommended", "latest-compatible", "awg-2026-08":
		return CoreBundle{
			ID: "awg-2026-08", ToolsVersion: "v3.1.20260812", ToolsCommit: "ee0f0a9aa34ff0a0da4b3433b9512781cfe02843", ToolsPackage: "1.0.20210914-0~202608130144+ee0f0a9~ubuntu24.04.1",
			KernelVersion: "v3.1.20260828", KernelCommit: "3c38e168beb7c60dec41dfe423d41555205a3dac", KernelPackage: "1.0.0-0~202608282205+3c38e16~ubuntu24.04.1",
			UserspaceVersion: "v3.1.20260828", UserspaceCommit: "b5928efb6ca19f0153958460c3d141f04abc5c2e",
		}, nil
	}
	return CoreBundle{}, terminalError("install.error.core.1")
}

func installedPackage(ctx context.Context, h Host, name string) string {
	raw, err := h.Output(ctx, []string{"dpkg-query", "-W", "-f=${db:Status-Status}\t${Version}", name}, 10*time.Second)
	if err != nil {
		return ""
	}
	status, version, ok := strings.Cut(strings.TrimSpace(raw), "\t")
	if !ok || status != "installed" {
		return ""
	}
	return version
}

// InspectCore records package identity and observable module facts separately.
// sysfs version/srcversion are NOT proof of a Git source commit.
func InspectCore(ctx context.Context, h Host, b CoreBundle) CoreReport {
	r := CoreReport{Requested: b, ToolsLocation: "host", ToolsPackage: installedPackage(ctx, h, "amneziawg-tools"), KernelPackage: installedPackage(ctx, h, "amneziawg-dkms")}
	if raw, err := h.Output(ctx, []string{"awg", "--version"}, 10*time.Second); err == nil {
		r.ToolsVersion = strings.TrimSpace(raw)
	}
	if raw, err := h.ReadFile("/sys/module/amneziawg/version"); err == nil {
		r.ModuleLoaded = true
		r.LoadedVersion = strings.TrimSpace(string(raw))
	}
	if raw, err := h.ReadFile("/sys/module/amneziawg/srcversion"); err == nil {
		r.ModuleLoaded = true
		r.LoadedSource = strings.TrimSpace(string(raw))
	}
	if raw, err := h.ReadFile("/proc/modules"); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(line, "amneziawg ") {
				r.ModuleLoaded = true
			}
		}
	}
	if raw, err := h.Output(ctx, []string{"modinfo", "-F", "srcversion", "amneziawg"}, 10*time.Second); err == nil {
		r.DiskSource = strings.TrimSpace(raw)
	}
	r.RebootRequired = r.ModuleLoaded && r.LoadedSource != "" && r.DiskSource != "" && r.LoadedSource != r.DiskSource
	r.ModuleIdentity = "unknown"
	if r.ModuleLoaded && r.LoadedSource != "" && r.DiskSource != "" {
		r.ModuleIdentity = "matches-disk"
		if r.RebootRequired {
			r.ModuleIdentity = "differs-from-disk"
		}
	}
	return r
}

func packageAvailable(ctx context.Context, h Host, name, version string) bool {
	raw, err := h.Output(ctx, []string{"apt-cache", "madison", name}, 15*time.Second)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(raw, "\n") {
		cols := strings.Split(line, "|")
		if len(cols) > 1 && strings.TrimSpace(cols[0]) == name && (version == "" || strings.TrimSpace(cols[1]) == version) {
			return true
		}
	}
	return false
}

// EnsurePrerequisites never upgrades/downgrades an installed AWG package or
// unloads a module. external explicitly leaves host module lifecycle to the
// operator; native tools remain mandatory. Unknown OS adapters are check-only.
func EnsurePrerequisites(ctx context.Context, h Host, p Plan, platform PlatformReport, b CoreBundle, policy PrerequisitePolicy, external bool, st *State, out io.Writer) (CoreReport, error) {
	r := InspectCore(ctx, h, b)
	r.ExternalModule = external
	if policy == "" {
		policy = PrerequisitesAuto
	}
	if policy != PrerequisitesAuto && policy != PrerequisitesCheck {
		return r, terminalError("install.error.core.2")
	}
	selected, err := SelectCore(b.ID)
	if err != nil || selected != b {
		return r, terminalError("install.error.core.3")
	}
	noble := platform.OS == "ubuntu" && platform.Version == "24.04"
	automatic := policy == PrerequisitesAuto && platform.AutomaticPackages && noble && platform.Init == "systemd"
	type dependency struct{ name, version string }
	var pending []dependency
	require := func(name, version string) error {
		current := installedPackage(ctx, h, name)
		if current != "" {
			if version != "" && current != version {
				return terminalError("install.error.core.4", name)
			}
			return nil
		}
		if !automatic {
			return terminalError("install.error.core.5", name)
		}
		pending = append(pending, dependency{name, version})
		return nil
	}
	if p.Mode == ModeNative && noble {
		for _, name := range []string{"iproute2", "nftables", "procps", "ca-certificates"} {
			if err := require(name, ""); err != nil {
				return r, err
			}
		}
		if err := require("amneziawg-tools", b.ToolsPackage); err != nil {
			return r, err
		}
	}
	if p.Mode == ModeDocker {
		if _, err := h.LookPath("docker"); err != nil {
			if err := require("docker.io", ""); err != nil {
				return r, err
			}
		}
		if err := h.Run(ctx, []string{"docker", "compose", "version"}, 30*time.Second); err != nil {
			// Noble's plugin recommends (does not require) docker.io. Disable
			// recommends and removals so an existing Docker CE engine is preserved.
			if !automatic {
				return r, terminalError("install.error.core.6")
			}
			if err := require("docker-compose-v2", ""); err != nil {
				return r, err
			}
		}
	}
	if !external && noble {
		if err := require("amneziawg-dkms", b.KernelPackage); err != nil {
			return r, err
		}
		if !r.ModuleLoaded || r.KernelPackage == "" {
			for _, name := range []string{"kmod", "dkms", "build-essential", "linux-headers-" + platform.Kernel} {
				if err := require(name, ""); err != nil {
					return r, err
				}
			}
		}
	}
	// On the explicit Ubuntu adapter, source preparation is a prerequisite
	// mutation. Core/deployment writes still wait for BOTH exact package pins.
	if len(pending) > 0 && automatic {
		if err := h.Run(ctx, []string{"apt-get", "update"}, longTimeout); err != nil {
			return r, terminalError("install.error.core.7")
		}
		needCore := false
		for _, dep := range pending {
			if dep.version != "" {
				needCore = true
			}
		}
		if needCore && (!packageAvailable(ctx, h, "amneziawg-tools", b.ToolsPackage) || !packageAvailable(ctx, h, "amneziawg-dkms", b.KernelPackage)) {
			if err := prepareUbuntuRepository(ctx, h, st); err != nil {
				return r, err
			}
		}
		if needCore && (!packageAvailable(ctx, h, "amneziawg-tools", b.ToolsPackage) || !packageAvailable(ctx, h, "amneziawg-dkms", b.KernelPackage)) {
			return r, terminalError("install.error.core.8")
		}
	}
	// Check the entire pending set before installing any runtime/core package.
	for _, dep := range pending {
		if !packageAvailable(ctx, h, dep.name, dep.version) {
			return r, terminalError("install.error.core.9", dep.name, dep.version)
		}
	}
	if len(pending) > 0 {
		args := []string{"apt-get", "install", "-y", "--no-install-recommends", "--no-upgrade", "--no-remove"}
		for _, dep := range pending {
			arg := dep.name
			if dep.version != "" {
				arg += "=" + dep.version
			}
			args = append(args, arg)
		}
		installErr := h.Run(ctx, args, longTimeout)
		for _, dep := range pending {
			if installedPackage(ctx, h, dep.name) != "" {
				st.PackagesInstalled = addUnique(st.PackagesInstalled, dep.name)
			}
		}
		if installErr != nil {
			return r, terminalError("install.error.core.10", installErr)
		}
	}
	requiredTools := []string{}
	if p.Mode == ModeNative {
		requiredTools = []string{"systemctl", "ip", "tc", "nft", "sysctl", "awg"}
	}
	if p.Mode == ModeDocker {
		requiredTools = []string{"docker"}
	}
	for _, tool := range requiredTools {
		if _, err := h.LookPath(tool); err != nil {
			return r, terminalError("install.error.core.11", tool)
		}
	}
	if p.Mode == ModeDocker {
		if err := h.Run(ctx, []string{"docker", "compose", "version"}, 30*time.Second); err != nil {
			return r, terminalError("install.error.core.12")
		}
		if err := h.Run(ctx, []string{"docker", "info"}, 30*time.Second); err != nil {
			return r, terminalError("install.error.core.13")
		}
	}
	r = InspectCore(ctx, h, b)
	r.ExternalModule = external
	if p.Mode == ModeNative && (noble && r.ToolsPackage != b.ToolsPackage || !strings.Contains(r.ToolsVersion, b.ToolsVersion)) {
		return r, terminalError("install.error.core.14")
	}
	if external {
		return r, nil
	}
	if noble && r.KernelPackage != b.KernelPackage {
		return r, terminalError("install.error.core.15")
	}
	if r.RebootRequired {
		return r, terminalError("install.error.core.16")
	}
	if !r.ModuleLoaded {
		if !automatic {
			return r, terminalError("install.error.core.17")
		}
		if err := h.Run(ctx, []string{"modprobe", "amneziawg"}, 30*time.Second); err != nil {
			// Rebuild only the selected module for this running kernel, never all DKMS modules.
			if err := h.Run(ctx, []string{"dkms", "install", "-m", "amneziawg", "-v", "1.0.0", "-k", platform.Kernel}, longTimeout); err != nil {
				return r, terminalError("install.error.core.18")
			}
			if err := h.Run(ctx, []string{"depmod", "-a", platform.Kernel}, time.Minute); err != nil {
				return r, terminalError("install.error.core.19")
			}
			if err := h.Run(ctx, []string{"modprobe", "amneziawg"}, 30*time.Second); err != nil {
				return r, terminalError("install.error.core.20")
			}
		}
		r = InspectCore(ctx, h, b)
		if !r.ModuleLoaded {
			return r, terminalError("install.error.core.21")
		}
	}
	if r.ModuleIdentity != "matches-disk" {
		return r, terminalError("install.error.core.22")
	}
	if automatic {
		if err := markModuleBootPersistence(h, st, out); err != nil {
			return r, err
		}
	}
	return r, nil
}

func prepareUbuntuRepository(ctx context.Context, h Host, st *State) error {
	// Only the supported adapter reaches here; never rewrite a PPA suite.
	var missing []string
	for _, name := range []string{"software-properties-common", "ca-certificates"} {
		if installedPackage(ctx, h, name) == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		args := append([]string{"apt-get", "install", "-y", "--no-install-recommends", "--no-upgrade", "--no-remove"}, missing...)
		err := h.Run(ctx, args, longTimeout)
		for _, name := range missing {
			if installedPackage(ctx, h, name) != "" {
				st.PackagesInstalled = addUnique(st.PackagesInstalled, name)
			}
		}
		if err != nil {
			return terminalError("install.error.core.24")
		}
	}
	policy, _ := h.Output(ctx, []string{"apt-cache", "policy"}, 15*time.Second)
	if !strings.Contains(policy, "ppa.launchpadcontent.net/amnezia/ppa/ubuntu") && !strings.Contains(policy, "ppa.launchpad.net/amnezia/ppa/ubuntu") {
		if err := h.Run(ctx, []string{"add-apt-repository", "-y", "ppa:amnezia/ppa"}, longTimeout); err != nil {
			return terminalError("install.error.core.25")
		}
		st.RepositoryChanges = addUnique(st.RepositoryChanges, "ppa:amnezia/ppa (Ubuntu 24.04 noble; retained on uninstall)")
	}
	if err := h.Run(ctx, []string{"apt-get", "update"}, longTimeout); err != nil {
		return terminalError("install.error.core.26")
	}
	return nil
}
