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
	ID                string `json:"id"`
	Source            string `json:"source"`
	ToolsVersion      string `json:"tools_version"`
	ToolsCommit       string `json:"tools_commit"`
	ToolsRepository   string `json:"tools_repository"`
	ToolsPackage      string `json:"tools_package"`
	KernelVersion     string `json:"kernel_version"`
	KernelCommit      string `json:"kernel_commit"`
	KernelRepository  string `json:"kernel_repository"`
	KernelDKMSVersion string `json:"kernel_dkms_version"`
	KernelPackage     string `json:"kernel_package"`
	UserspaceVersion  string `json:"userspace_version"`
	UserspaceCommit   string `json:"userspace_commit"`
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
	ToolsSource    string     `json:"tools_source,omitempty"`
	KernelSource   string     `json:"kernel_source,omitempty"`
	KernelDKMS     string     `json:"kernel_dkms_version,omitempty"`
}

func SelectCore(selector string) (CoreBundle, error) {
	switch selector {
	case "", "recommended", "latest-compatible", "awg-2026-09":
		return CoreBundle{
			ID: "awg-2026-09", Source: coreSourceGitHub,
			ToolsVersion: "v3.1.20260812", ToolsCommit: "ee0f0a9aa34ff0a0da4b3433b9512781cfe02843", ToolsRepository: "https://github.com/amnezia-vpn/amneziawg-tools.git",
			KernelVersion: "v3.1.20260906", KernelCommit: "4569c4c67f3a57414969260cafbbd04694fbaae0", KernelRepository: "https://github.com/amnezia-vpn/amneziawg-linux-kernel-module.git", KernelDKMSVersion: "1.0.0-wgguard.20260906",
			UserspaceVersion: "v3.1.20260828", UserspaceCommit: "b5928efb6ca19f0153958460c3d141f04abc5c2e",
		}, nil
	case "awg-2026-08":
		return CoreBundle{
			ID: "awg-2026-08", Source: coreSourcePackage, ToolsVersion: "v3.1.20260812", ToolsCommit: "ee0f0a9aa34ff0a0da4b3433b9512781cfe02843", ToolsRepository: "https://github.com/amnezia-vpn/amneziawg-tools.git", ToolsPackage: "1.0.20210914-0~202608130144+ee0f0a9~ubuntu24.04.1",
			KernelVersion: "v3.1.20260828", KernelCommit: "3c38e168beb7c60dec41dfe423d41555205a3dac", KernelRepository: "https://github.com/amnezia-vpn/amneziawg-linux-kernel-module.git", KernelPackage: "1.0.0-0~202608282205+3c38e16~ubuntu24.04.1",
			UserspaceVersion: "v3.1.20260828", UserspaceCommit: "b5928efb6ca19f0153958460c3d141f04abc5c2e",
		}, nil
	}
	return CoreBundle{}, terminalError("install.error.core.1")
}

func coreReportMatchesBundle(r CoreReport, b CoreBundle) bool {
	if !strings.Contains(r.ToolsVersion, b.ToolsVersion) {
		return false
	}
	if b.Source == coreSourceGitHub {
		return r.ToolsSource == coreSourceGitHub && r.KernelSource == coreSourceGitHub && r.KernelDKMS == b.KernelDKMSVersion
	}
	if r.ToolsLocation == "container" && r.ToolsSource == coreSourceGitHub {
		return r.KernelPackage == b.KernelPackage
	}
	return r.ToolsPackage == b.ToolsPackage && r.KernelPackage == b.KernelPackage
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
	if b.Source == coreSourceGitHub {
		if sourceInstalled(h, toolsInstalledMarker(b), b.ToolsCommit) {
			r.ToolsSource = coreSourceGitHub
		}
		if sourceInstalled(h, kernelInstalledMarker(b), b.KernelCommit) {
			r.KernelSource = coreSourceGitHub
			r.KernelDKMS = b.KernelDKMSVersion
		}
	}
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
// operator; native tools remain mandatory.
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
	managedUbuntu := platform.OS == "ubuntu" && supportedUbuntuVersion(platform.Version) && platform.Arch == "amd64"
	automatic := policy == PrerequisitesAuto && platform.AutomaticPackages && managedUbuntu && platform.Init == "systemd"
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
	if p.Mode == ModeNative && managedUbuntu {
		for _, name := range []string{"iproute2", "nftables", "procps", "ca-certificates"} {
			if err := require(name, ""); err != nil {
				return r, err
			}
		}
		if b.Source == coreSourcePackage {
			if err := require("amneziawg-tools", b.ToolsPackage); err != nil {
				return r, err
			}
		}
	}
	if p.Mode == ModeDocker {
		if _, err := h.LookPath("docker"); err != nil {
			if err := require("docker.io", ""); err != nil {
				return r, err
			}
		}
		if err := runQuiet(ctx, h, []string{"docker", "compose", "version"}, 30*time.Second); err != nil {
			// Ubuntu's plugin recommends (does not require) docker.io. Disable
			// recommends and removals so an existing Docker CE engine is preserved.
			if !automatic {
				return r, terminalError("install.error.core.6")
			}
			if err := require("docker-compose-v2", ""); err != nil {
				return r, err
			}
		}
	}
	if !external && managedUbuntu {
		if b.Source == coreSourcePackage {
			if err := require("amneziawg-dkms", b.KernelPackage); err != nil {
				return r, err
			}
		}
		if !r.ModuleLoaded || r.KernelPackage == "" {
			for _, name := range []string{"kmod", "dkms", "build-essential", "linux-headers-" + platform.Kernel} {
				if err := require(name, ""); err != nil {
					return r, err
				}
			}
		}
	}
	if b.Source == coreSourceGitHub && managedUbuntu && (p.Mode == ModeNative || !external) {
		for _, name := range []string{"git", "build-essential", "ca-certificates"} {
			if err := require(name, ""); err != nil {
				return r, err
			}
		}
	}
	// On the explicit Ubuntu adapter, source preparation is a prerequisite
	// mutation. Core/deployment writes still wait for BOTH exact package pins.
	if len(pending) > 0 && automatic {
		if err := runQuiet(ctx, h, []string{"apt-get", "update"}, longTimeout); err != nil {
			return r, terminalError("install.error.core.7")
		}
		needCore := false
		for _, dep := range pending {
			if dep.version != "" {
				needCore = true
			}
		}
		if b.Source == coreSourcePackage && needCore && (!packageAvailable(ctx, h, "amneziawg-tools", b.ToolsPackage) || !packageAvailable(ctx, h, "amneziawg-dkms", b.KernelPackage)) {
			if err := prepareUbuntuRepository(ctx, h, st); err != nil {
				return r, err
			}
		}
		if b.Source == coreSourcePackage && needCore && (!packageAvailable(ctx, h, "amneziawg-tools", b.ToolsPackage) || !packageAvailable(ctx, h, "amneziawg-dkms", b.KernelPackage)) {
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
		installErr := runQuiet(ctx, h, args, longTimeout)
		for _, dep := range pending {
			if installedPackage(ctx, h, dep.name) != "" {
				st.PackagesInstalled = addUnique(st.PackagesInstalled, dep.name)
			}
		}
		if installErr != nil {
			return r, terminalError("install.error.core.10", installErr)
		}
	}
	if b.Source == coreSourceGitHub && automatic {
		step(out, "AmneziaWG core")
		progress(out, "core_source", b.ID)
		if p.Mode == ModeNative {
			if err := ensurePinnedTools(ctx, h, b); err != nil {
				return r, err
			}
		}
		if !external {
			if err := ensurePinnedKernel(ctx, h, platform.Kernel, b); err != nil {
				return r, err
			}
		}
		progress(out, "core_ready", b.ID)
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
		if err := runQuiet(ctx, h, []string{"docker", "compose", "version"}, 30*time.Second); err != nil {
			return r, terminalError("install.error.core.12")
		}
		if err := runQuiet(ctx, h, []string{"docker", "info"}, 30*time.Second); err != nil {
			if !automatic {
				return r, terminalError("install.error.core.13")
			}
			progress(out, "docker_start")
			if startErr := runQuiet(ctx, h, []string{"systemctl", "start", "docker.service"}, time.Minute); startErr != nil {
				return r, terminalError("install.error.core.13")
			}
			if retryErr := runQuiet(ctx, h, []string{"docker", "info"}, 30*time.Second); retryErr != nil {
				return r, terminalError("install.error.core.13")
			}
		}
	}
	r = InspectCore(ctx, h, b)
	r.ExternalModule = external
	if p.Mode == ModeNative && (b.Source == coreSourcePackage && managedUbuntu && r.ToolsPackage != b.ToolsPackage || b.Source == coreSourceGitHub && managedUbuntu && r.ToolsSource != coreSourceGitHub || !strings.Contains(r.ToolsVersion, b.ToolsVersion)) {
		return r, terminalError("install.error.core.14")
	}
	if external {
		return r, nil
	}
	if managedUbuntu && b.Source == coreSourcePackage && r.KernelPackage != b.KernelPackage {
		return r, terminalError("install.error.core.15")
	}
	if managedUbuntu && b.Source == coreSourceGitHub && (r.KernelSource != coreSourceGitHub || r.KernelDKMS != b.KernelDKMSVersion) {
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
			dkmsVersion := "1.0.0"
			if b.KernelDKMSVersion != "" {
				dkmsVersion = b.KernelDKMSVersion
			}
			if err := runQuiet(ctx, h, []string{"dkms", "install", "-m", "amneziawg", "-v", dkmsVersion, "-k", platform.Kernel}, longTimeout); err != nil {
				return r, terminalError("install.error.core.18")
			}
			if err := runQuiet(ctx, h, []string{"depmod", "-a", platform.Kernel}, time.Minute); err != nil {
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
	// Only the supported Ubuntu adapter reaches here. add-apt-repository selects
	// the host suite; exact package availability is verified before installation.
	var missing []string
	for _, name := range []string{"software-properties-common", "ca-certificates"} {
		if installedPackage(ctx, h, name) == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		args := append([]string{"apt-get", "install", "-y", "--no-install-recommends", "--no-upgrade", "--no-remove"}, missing...)
		err := runQuiet(ctx, h, args, longTimeout)
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
		if err := runQuiet(ctx, h, []string{"add-apt-repository", "-y", "ppa:amnezia/ppa"}, longTimeout); err != nil {
			return terminalError("install.error.core.25")
		}
		st.RepositoryChanges = addUnique(st.RepositoryChanges, "ppa:amnezia/ppa (host Ubuntu suite; retained on uninstall)")
	}
	if err := runQuiet(ctx, h, []string{"apt-get", "update"}, longTimeout); err != nil {
		return terminalError("install.error.core.26")
	}
	return nil
}
