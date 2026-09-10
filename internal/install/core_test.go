package install

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

type packageHost struct {
	*memHost
	installed, available map[string]string
	installedArgs        []string
	metadataQueries      []string
}

func newPackageHost() *packageHost {
	return &packageHost{memHost: newMemHost(), installed: map[string]string{"procps": "system", "software-properties-common": "system", "iproute2": "system", "nftables": "system", "iptables": "system", "ca-certificates": "system", "git": "system", "kmod": "system", "dkms": "system", "build-essential": "system", "linux-headers-6.8.0-138-generic": "system"}, available: map[string]string{
		"amneziawg-tools": "1.0.20210914-0~202608130144+ee0f0a9~ubuntu24.04.1",
		"amneziawg-dkms":  "1.0.0-0~202608282205+3c38e16~ubuntu24.04.1",
		"iptables":        "1.8.10-3ubuntu2",
	}}
}
func (h *packageHost) Output(ctx context.Context, a []string, d time.Duration) (string, error) {
	if a[0] == "dpkg-query" {
		v := h.installed[a[len(a)-1]]
		if v == "" {
			return "", fmt.Errorf("not installed")
		}
		return "installed\t" + v, nil
	}
	if a[0] == "apt-cache" {
		h.metadataQueries = append(h.metadataQueries, a[len(a)-1])
		v := h.available[a[len(a)-1]]
		if v == "" {
			return "", fmt.Errorf("unavailable")
		}
		return a[len(a)-1] + " | " + v + " | noble/main", nil
	}
	return h.memHost.Output(ctx, a, d)
}

func TestInstalledExactCoreReuseDoesNotRequireRepositoryAccess(t *testing.T) {
	h := newPackageHost()
	b, _ := SelectCore("awg-2026-08")
	h.installed["amneziawg-tools"] = b.ToolsPackage
	h.installed["amneziawg-dkms"] = b.KernelPackage
	h.available = map[string]string{}
	h.failCmd["apt-get"] = fmt.Errorf("offline")
	h.files["/sys/module/amneziawg/version"] = memFile{data: []byte("3.1.20260812")}
	h.files["/sys/module/amneziawg/srcversion"] = memFile{data: []byte("MATCHINGBUILD")}
	r, _ := InspectPlatform(context.Background(), h)
	report, err := EnsurePrerequisites(context.Background(), h, Plan{Mode: ModeNative}, r, b, PrerequisitesAuto, false, &State{}, io.Discard)
	if err != nil || report.ModuleIdentity != "matches-disk" {
		t.Fatalf("validated installed bundle cannot be reused offline: %+v %v", report, err)
	}
	if len(h.metadataQueries) != 0 || h.ran("apt-get") || h.ran("add-apt-repository") || len(h.installedArgs) != 0 {
		t.Fatal("installed exact bundle unnecessarily required repository access")
	}
}

func TestNativePrerequisitesInstallIPTablesCompatibilityTool(t *testing.T) {
	h := newPackageHost()
	delete(h.installed, "iptables")
	b, _ := SelectCore("awg-2026-08")
	h.installed["amneziawg-tools"] = b.ToolsPackage
	h.installed["amneziawg-dkms"] = b.KernelPackage
	h.files["/sys/module/amneziawg/version"] = memFile{data: []byte("3.1.20260812")}
	h.files["/sys/module/amneziawg/srcversion"] = memFile{data: []byte("MATCHINGBUILD")}
	r, _ := InspectPlatform(context.Background(), h)
	if _, err := EnsurePrerequisites(context.Background(), h, Plan{Mode: ModeNative}, r, b,
		PrerequisitesAuto, false, &State{}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !contains(h.installedArgs, "iptables") {
		t.Fatalf("iptables compatibility tool not installed: %v", h.installedArgs)
	}
}

func TestAnyMissingCorePackageRequiresBothMetadataPins(t *testing.T) {
	for _, missing := range []string{"amneziawg-tools", "amneziawg-dkms"} {
		for _, otherAvailable := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/other-available=%t", missing, otherAvailable), func(t *testing.T) {
				h := newPackageHost()
				b, _ := SelectCore("awg-2026-08")
				h.installed["amneziawg-tools"] = b.ToolsPackage
				h.installed["amneziawg-dkms"] = b.KernelPackage
				delete(h.installed, missing)
				h.files["/sys/module/amneziawg/version"] = memFile{data: []byte("3.1.20260812")}
				h.files["/sys/module/amneziawg/srcversion"] = memFile{data: []byte("MATCHINGBUILD")}
				other := "amneziawg-tools"
				if missing == other {
					other = "amneziawg-dkms"
				}
				if !otherAvailable {
					delete(h.available, other)
				}
				r, _ := InspectPlatform(context.Background(), h)
				_, err := EnsurePrerequisites(context.Background(), h, Plan{Mode: ModeNative}, r, b, PrerequisitesAuto, false, &State{}, io.Discard)
				if !otherAvailable {
					if err == nil || len(h.installedArgs) != 0 {
						t.Fatal("installed a core package without the other exact metadata pin")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if !contains(h.metadataQueries, "amneziawg-tools") || !contains(h.metadataQueries, "amneziawg-dkms") {
					t.Fatal("did not check both metadata pins")
				}
				if len(h.installedArgs) != 1 || !strings.HasPrefix(h.installedArgs[0], missing+"=") {
					t.Fatalf("unexpected package changes: %v", h.installedArgs)
				}
			})
		}
	}
}
func (h *packageHost) Run(ctx context.Context, a []string, d time.Duration) error {
	if a[0] == "modprobe" && len(a) == 2 && a[1] == "amneziawg" {
		h.files["/sys/module/amneziawg/version"] = memFile{data: []byte("3.1.20260812")}
	}
	if len(a) > 2 && a[0] == "apt-get" && a[1] == "install" {
		for _, arg := range a[2:] {
			if strings.HasPrefix(arg, "-") {
				continue
			}
			h.installedArgs = append(h.installedArgs, arg)
			name, v, _ := strings.Cut(arg, "=")
			if v == "" {
				v = "system"
			}
			h.installed[name] = v
		}
	}
	return h.memHost.Run(ctx, a, d)
}

func TestCoreCatalogRejectsUncataloguedVersions(t *testing.T) {
	for _, selector := range []string{"recommended", "latest-compatible", "awg-2026-09"} {
		b, err := SelectCore(selector)
		if err != nil || b.ID != "awg-2026-09" || b.ToolsCommit != "ee0f0a9aa34ff0a0da4b3433b9512781cfe02843" || b.KernelCommit != "4569c4c67f3a57414969260cafbbd04694fbaae0" {
			t.Fatalf("catalog selection failed: %+v %v", b, err)
		}
		if b.ToolsRepository != "https://github.com/amnezia-vpn/amneziawg-tools.git" || b.KernelRepository != "https://github.com/amnezia-vpn/amneziawg-linux-kernel-module.git" || b.KernelDKMSVersion != "1.0.0-wgguard.20260906" {
			t.Fatalf("catalog source provenance incomplete: %+v", b)
		}
	}
	legacy, err := SelectCore("awg-2026-08")
	if err != nil || legacy.KernelCommit != "3c38e168beb7c60dec41dfe423d41555205a3dac" {
		t.Fatalf("legacy installed bundle is no longer recognizable: %+v %v", legacy, err)
	}
	if _, err := SelectCore("upstream-main"); err == nil {
		t.Fatal("arbitrary core accepted")
	}
}

type sourceCoreHost struct {
	*packageHost
	revision      string
	dkmsInstalled bool
}

type quietSourceCoreHost struct {
	*sourceCoreHost
	quiet [][]string
}

func (h *quietSourceCoreHost) RunQuiet(ctx context.Context, a []string, d time.Duration) error {
	h.quiet = append(h.quiet, append([]string(nil), a...))
	return h.Run(ctx, a, d)
}

func newSourceCoreHost() *sourceCoreHost {
	h := &sourceCoreHost{packageHost: newPackageHost(), revision: "4569c4c67f3a57414969260cafbbd04694fbaae0"}
	delete(h.available, "amneziawg-tools")
	delete(h.available, "amneziawg-dkms")
	return h
}

func (h *sourceCoreHost) Output(ctx context.Context, a []string, d time.Duration) (string, error) {
	if len(a) >= 4 && a[0] == "git" && a[1] == "-C" && a[3] == "rev-parse" {
		h.commands = append(h.commands, memCmd{argv: a})
		if strings.Contains(a[2], "/tools") || strings.Contains(a[2], "\\tools") {
			return "ee0f0a9aa34ff0a0da4b3433b9512781cfe02843", nil
		}
		return h.revision, nil
	}
	if len(a) > 1 && a[0] == "dkms" && a[1] == "status" {
		h.commands = append(h.commands, memCmd{argv: a})
		if h.dkmsInstalled {
			return "amneziawg/1.0.0-wgguard.20260906, 6.8.0-138-generic, x86_64: installed", nil
		}
		return "", fmt.Errorf("not installed")
	}
	return h.packageHost.Output(ctx, a, d)
}

func (h *sourceCoreHost) Run(ctx context.Context, a []string, d time.Duration) error {
	if len(a) > 0 && a[0] == "make" {
		h.files[ManagedAWGBuildPath] = memFile{data: []byte("reviewed awg binary"), perm: 0o755}
	}
	if len(a) > 1 && a[0] == "dkms" && a[1] == "install" {
		h.dkmsInstalled = true
	}
	return h.packageHost.Run(ctx, a, d)
}

func TestMissingAWGPackagesUsePinnedGitHubSource(t *testing.T) {
	h := newSourceCoreHost()
	b, _ := SelectCore("recommended")
	r, _ := InspectPlatform(context.Background(), h)
	st := &State{}
	_, err := EnsurePrerequisites(context.Background(), h, Plan{Mode: ModeNative}, r, b, PrerequisitesAuto, false, st, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if h.ran("add-apt-repository") {
		t.Fatal("mutable PPA remained on the critical installation path")
	}
	if !h.ran("git", "-c", "advice.detachedHead=false", "clone", "--quiet", "--depth", "1", "--branch", "v3.1.20260906") {
		t.Fatalf("kernel source was not fetched from the reviewed tag: %v", h.ranCommands())
	}
	if !h.ran("dkms", "install", "-m", "amneziawg", "-v", b.KernelDKMSVersion, "-k", r.Kernel) {
		t.Fatalf("reviewed module was not installed with DKMS: %v", h.ranCommands())
	}
	if !h.ran("git", "-c", "advice.detachedHead=false", "clone", "--quiet", "--depth", "1", "--branch", "v3.1.20260812") {
		t.Fatalf("tools source was not fetched from the reviewed tag: %v", h.ranCommands())
	}
	if got := string(h.files[ManagedAWGBinaryPath].data); got != "reviewed awg binary" {
		t.Fatalf("managed awg binary = %q", got)
	}
	for _, arg := range h.installedArgs {
		if strings.HasPrefix(arg, "amneziawg-") {
			t.Fatalf("AWG package unexpectedly installed from PPA: %q", arg)
		}
	}
	dkmsConfig := string(h.files["/usr/src/amneziawg-"+b.KernelDKMSVersion+"/dkms.conf"].data)
	if strings.Contains(dkmsConfig, "REMAKE_INITRD") || !strings.Contains(dkmsConfig, `PACKAGE_VERSION="`+b.KernelDKMSVersion+`"`) {
		t.Fatalf("managed DKMS config is noisy or unversioned:\n%s", dkmsConfig)
	}
}

func TestSourceCoreKeepsNoisyBuildCommandsOutOfTheTerminal(t *testing.T) {
	h := &quietSourceCoreHost{sourceCoreHost: newSourceCoreHost()}
	b, _ := SelectCore("recommended")
	r, _ := InspectPlatform(context.Background(), h)
	if _, err := EnsurePrerequisites(context.Background(), h, Plan{Mode: ModeDocker}, r, b, PrerequisitesAuto, false, &State{}, io.Discard); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"git", "make", "dkms"} {
		found := false
		for _, argv := range h.quiet {
			if len(argv) > 0 && argv[0] == command {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("%s did not use the quiet installer runner: %v", command, h.quiet)
		}
	}
}

func TestSourceRevisionMismatchStopsBeforeBuildOrDKMS(t *testing.T) {
	h := newSourceCoreHost()
	h.revision = strings.Repeat("f", 40)
	b, _ := SelectCore("recommended")
	r, _ := InspectPlatform(context.Background(), h)
	_, err := EnsurePrerequisites(context.Background(), h, Plan{Mode: ModeDocker}, r, b, PrerequisitesAuto, false, &State{}, io.Discard)
	if err == nil {
		t.Fatal("mismatched upstream revision accepted")
	}
	if h.ran("make") || h.ran("dkms", "add") || h.ran("dkms", "install") {
		t.Fatalf("unverified source reached a build boundary: %v", h.ranCommands())
	}
}

func TestSourceInstallRecordsMissingGitAsValidPrerequisite(t *testing.T) {
	h := newSourceCoreHost()
	delete(h.installed, "git")
	h.available["git"] = "system"
	b, _ := SelectCore("recommended")
	r, _ := InspectPlatform(context.Background(), h)
	st := &State{Schema: StateSchema, Mode: ModeNative, ConfigPath: ConfigPath, DataDir: DataDir, BinPath: BinPath, UnitPath: UnitPath}
	if _, err := EnsurePrerequisites(context.Background(), h, Plan{Mode: ModeNative}, r, b, PrerequisitesAuto, false, st, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !contains(st.PackagesInstalled, "git") {
		t.Fatal("installer-installed git was not recorded")
	}
	if err := validateState(st); err != nil {
		t.Fatalf("valid source prerequisite ownership was rejected: %v", err)
	}
}

func TestCoreMissingPackagesInstallExactAvailableVersions(t *testing.T) {
	h := newPackageHost()
	b, _ := SelectCore("awg-2026-08")
	r, _ := InspectPlatform(context.Background(), h)
	st := &State{}
	_, err := EnsurePrerequisites(context.Background(), h, Plan{Mode: ModeNative}, r, b, PrerequisitesAuto, false, st, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(h.installedArgs, "amneziawg-tools=1.0.20210914-0~202608130144+ee0f0a9~ubuntu24.04.1") || !contains(h.installedArgs, "amneziawg-dkms=1.0.0-0~202608282205+3c38e16~ubuntu24.04.1") {
		t.Fatalf("core not installed with exact versions: %v", h.installedArgs)
	}
	if !contains(st.PackagesInstalled, "amneziawg-tools") {
		t.Fatal("new package ownership not recorded")
	}
}

func TestCoreAvailabilityAndVersionConflictPreventInstalls(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(fmt.Sprint(conflict), func(t *testing.T) {
			h := newPackageHost()
			if conflict {
				h.installed["amneziawg-tools"] = "foreign-version"
			} else {
				delete(h.available, "amneziawg-dkms")
			}
			b, _ := SelectCore("awg-2026-08")
			r, _ := InspectPlatform(context.Background(), h)
			_, err := EnsurePrerequisites(context.Background(), h, Plan{Mode: ModeNative}, r, b, PrerequisitesAuto, false, &State{}, io.Discard)
			if err == nil {
				t.Fatal("unavailable/conflicting core accepted")
			}
			if len(h.installedArgs) > 0 {
				t.Fatalf("installed before entire core availability check: %v", h.installedArgs)
			}
		})
	}
}

func TestCoreCheckOnlyDoesNotInstallOrLoadModule(t *testing.T) {
	h := newPackageHost()
	b, _ := SelectCore("awg-2026-08")
	r, _ := InspectPlatform(context.Background(), h)
	r.OS = "debian"
	r.Version = "12"
	r.AutomaticPackages = false
	_, err := EnsurePrerequisites(context.Background(), h, Plan{Mode: ModeNative}, r, b, PrerequisitesAuto, false, &State{}, io.Discard)
	if err == nil {
		t.Fatal("missing manual prerequisites accepted")
	}
	if h.ran("apt-get") || h.ran("add-apt-repository") || h.ran("modprobe") {
		t.Fatal("unsupported automatic adapter mutated host")
	}
}

func TestCoreReportsLoadedMismatchWithoutUnloading(t *testing.T) {
	h := newPackageHost()
	b, _ := SelectCore("awg-2026-08")
	h.installed["amneziawg-tools"] = b.ToolsPackage
	h.installed["amneziawg-dkms"] = b.KernelPackage
	h.files["/sys/module/amneziawg/srcversion"] = memFile{data: []byte("OLD")}
	h.files["/sys/module/amneziawg/version"] = memFile{data: []byte("3.1.20260812")}
	h.output["modinfo"] = "NEW"
	r, _ := InspectPlatform(context.Background(), h)
	report, err := EnsurePrerequisites(context.Background(), h, Plan{Mode: ModeNative}, r, b, PrerequisitesAuto, false, &State{}, io.Discard)
	if err == nil || !report.RebootRequired {
		t.Fatalf("loaded mismatch not reported: %+v %v", report, err)
	}
	if h.ran("rmmod") || h.ran("modprobe", "-r") {
		t.Fatal("active module unloaded")
	}
}

func TestCoreUnknownLoadedIdentityIsNotReady(t *testing.T) {
	h := newPackageHost()
	b, _ := SelectCore("awg-2026-08")
	h.installed["amneziawg-tools"] = b.ToolsPackage
	h.installed["amneziawg-dkms"] = b.KernelPackage
	h.files["/sys/module/amneziawg/version"] = memFile{data: []byte("3.1.20260812")}
	r, _ := InspectPlatform(context.Background(), h)
	report, err := EnsurePrerequisites(context.Background(), h, Plan{Mode: ModeNative}, r, b, PrerequisitesAuto, false, &State{}, io.Discard)
	if err == nil {
		t.Fatalf("unknown loaded build certified: %+v", report)
	}
}

func TestUbuntuRepositoryPreparationPrecedesExactCoreInstall(t *testing.T) {
	h := newPackageHost()
	delete(h.available, "amneziawg-dkms")
	b, _ := SelectCore("awg-2026-08")
	r, _ := InspectPlatform(context.Background(), h)
	st := &State{}
	_, err := EnsurePrerequisites(context.Background(), h, Plan{Mode: ModeNative}, r, b, PrerequisitesAuto, false, st, io.Discard)
	if err == nil {
		t.Fatal("missing pin accepted")
	}
	if !h.ran("add-apt-repository", "-y", "ppa:amnezia/ppa") || !h.ran("apt-get", "update") {
		t.Fatal("missing documented Ubuntu repository preparation")
	}
	if len(h.installedArgs) > 0 {
		t.Fatal("AWG installed before exact package availability")
	}
	if len(st.RepositoryChanges) != 1 {
		t.Fatal("repository preparation not recorded")
	}
}

func TestOtherLinuxExternalCoreChecksToolsWithoutUbuntuPackages(t *testing.T) {
	h := newPackageHost()
	h.installed = map[string]string{}
	b, _ := SelectCore("recommended")
	r := PlatformReport{OS: "debian", Version: "12", Arch: "amd64", Init: "systemd", Kernel: "6.1.0"}
	_, err := EnsurePrerequisites(context.Background(), h, Plan{Mode: ModeNative}, r, b, PrerequisitesCheck, true, &State{}, io.Discard)
	if err != nil {
		t.Fatalf("manually supplied compatible tools rejected: %v", err)
	}
	if h.ran("apt-get") || h.ran("add-apt-repository") || h.ran("modprobe") {
		t.Fatal("manual route mutated prerequisites")
	}
	h.failCmd["awg"] = fmt.Errorf("missing")
	if _, err := EnsurePrerequisites(context.Background(), h, Plan{Mode: ModeNative}, r, b, PrerequisitesCheck, true, &State{}, io.Discard); err == nil {
		t.Fatal("external module bypassed required tools")
	}
}

type missingDockerHost struct{ *packageHost }

type quietMissingDockerHost struct {
	*missingDockerHost
	quiet [][]string
}

func (h *quietMissingDockerHost) RunQuiet(ctx context.Context, a []string, d time.Duration) error {
	h.quiet = append(h.quiet, append([]string(nil), a...))
	return h.Run(ctx, a, d)
}

func (h *missingDockerHost) LookPath(name string) (string, error) {
	if name == "docker" && h.installed["docker.io"] == "" {
		return "", fmt.Errorf("missing")
	}
	return h.memHost.LookPath(name)
}
func (h *missingDockerHost) Run(ctx context.Context, a []string, d time.Duration) error {
	if a[0] == "docker" && h.installed["docker.io"] == "" {
		return fmt.Errorf("missing docker")
	}
	return h.packageHost.Run(ctx, a, d)
}
func TestDockerMissingDependenciesUseUbuntuAdapter(t *testing.T) {
	h := &missingDockerHost{newPackageHost()}
	h.available["docker.io"] = "system"
	h.available["docker-compose-v2"] = "system"
	b, _ := SelectCore("recommended")
	r, _ := InspectPlatform(context.Background(), h)
	st := &State{}
	_, err := EnsurePrerequisites(context.Background(), h, Plan{Mode: ModeDocker}, r, b, PrerequisitesAuto, true, st, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(h.installedArgs, "docker.io") || !contains(h.installedArgs, "docker-compose-v2") {
		t.Fatal("missing Docker prerequisites not installed")
	}
	if !h.ran("docker", "info") {
		t.Fatal("daemon not checked")
	}
}

type inactiveDockerHost struct {
	*packageHost
	started bool
}

func (h *inactiveDockerHost) Run(ctx context.Context, argv []string, timeout time.Duration) error {
	if len(argv) > 1 && argv[0] == "docker" && argv[1] == "info" && !h.started {
		return fmt.Errorf("daemon inactive")
	}
	if len(argv) > 2 && argv[0] == "systemctl" && argv[1] == "start" && argv[2] == "docker.service" {
		h.started = true
	}
	return h.packageHost.Run(ctx, argv, timeout)
}

func TestDockerModeStartsInactiveSystemdDaemon(t *testing.T) {
	h := &inactiveDockerHost{packageHost: newPackageHost()}
	h.installed["docker.io"] = "system"
	h.installed["docker-compose-v2"] = "system"
	b, _ := SelectCore("recommended")
	r, _ := InspectPlatform(context.Background(), h)
	if _, err := EnsurePrerequisites(context.Background(), h, Plan{Mode: ModeDocker}, r, b, PrerequisitesAuto, true, &State{}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !h.started || !h.ran("systemctl", "daemon-reload") || !h.ran("systemctl", "restart", "docker.socket") || !h.ran("systemctl", "start", "docker.service") {
		t.Fatal("inactive Docker daemon was not started")
	}
}

func TestUbuntuPackageSetupUsesQuietInstallerRunner(t *testing.T) {
	h := &quietMissingDockerHost{missingDockerHost: &missingDockerHost{newPackageHost()}}
	h.available["docker.io"] = "system"
	h.available["docker-compose-v2"] = "system"
	b, _ := SelectCore("recommended")
	r, _ := InspectPlatform(context.Background(), h)
	if _, err := EnsurePrerequisites(context.Background(), h, Plan{Mode: ModeDocker}, r, b, PrerequisitesAuto, true, &State{}, io.Discard); err != nil {
		t.Fatal(err)
	}
	for _, subcommand := range []string{"update", "install"} {
		found := false
		for _, argv := range h.quiet {
			if len(argv) > 1 && argv[0] == "apt-get" && argv[1] == subcommand {
				found = true
			}
		}
		if !found {
			t.Fatalf("apt-get %s bypassed quiet runner: %v", subcommand, h.quiet)
		}
	}
}

type composeMissingHost struct{ *packageHost }

func (h *composeMissingHost) Run(ctx context.Context, a []string, d time.Duration) error {
	if len(a) > 2 && a[0] == "docker" && a[1] == "compose" && h.installed["docker-compose-v2"] == "" {
		return fmt.Errorf("missing compose")
	}
	return h.packageHost.Run(ctx, a, d)
}
func TestExistingDockerGetsOnlyMissingComposeWithNoRemoval(t *testing.T) {
	h := &composeMissingHost{newPackageHost()}
	h.available["docker-compose-v2"] = "system"
	b, _ := SelectCore("recommended")
	r, _ := InspectPlatform(context.Background(), h)
	_, err := EnsurePrerequisites(context.Background(), h, Plan{Mode: ModeDocker}, r, b, PrerequisitesAuto, true, &State{}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if contains(h.installedArgs, "docker.io") || !contains(h.installedArgs, "docker-compose-v2") {
		t.Fatal("existing Docker engine replaced or Compose omitted")
	}
	for _, command := range h.commands {
		if len(command.argv) > 1 && command.argv[0] == "apt-get" && command.argv[1] == "install" {
			if !contains(command.argv, "--no-remove") || !contains(command.argv, "--no-install-recommends") {
				t.Fatal("package resolver may remove engine or install recommended docker.io")
			}
		}
	}
}

func TestInstalledCoreReportsContainerToolsAndHostModule(t *testing.T) {
	h := newMemHost()
	h.files[StatePath] = memFile{data: []byte(`{"schema":1,"mode":"docker","config_path":"/etc/wg-guard/wg-guard.toml","data_dir":"/var/lib/wg-guard","compose_path":"/etc/wg-guard/compose.yaml","binary_path":"/usr/local/bin/wg-guard"}`)}
	h.output["docker exec wg-guard awg --version"] = "amneziawg-tools v3.1.20260812"
	h.output["docker exec wg-guard dpkg-query -W -f=${db:Status-Status}\t${Version} amneziawg-tools"] = "installed\tcontainer-package"
	r, err := InspectInstalledCore(context.Background(), h)
	if err != nil || r.Requested.ID != "awg-2026-08" || r.ToolsLocation != "container" || r.ToolsPackage != "container-package" || r.KernelPackage != "1.0.0-0~202608282205+3c38e16~ubuntu24.04.1" {
		t.Fatalf("incorrect core observation: %+v %v", r, err)
	}
}

func TestSourceDockerCoreUsesRecordedImageBundleIdentity(t *testing.T) {
	h := installedFixture(t, ModeDocker)
	h.output["docker inspect --format {{ index .Config.Labels \"io.wg-guard.awg-tools.commit\" }} wg-guard"] = "ee0f0a9aa34ff0a0da4b3433b9512781cfe02843\n"
	h.output["docker exec wg-guard awg --version"] = "amneziawg-tools v3.1.20260812"
	r, err := InspectInstalledCore(context.Background(), h)
	if err != nil {
		t.Fatal(err)
	}
	if r.ToolsSource != coreSourceGitHub || !coreReportMatchesBundle(r, r.Requested) {
		t.Fatalf("source-backed container core identity was not proven: %+v", r)
	}
}

func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
