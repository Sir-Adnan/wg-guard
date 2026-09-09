package install

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"time"
)

const (
	coreSourcePackage = "ubuntu-package"
	coreSourceGitHub  = "github-source"

	CoreCacheDir         = "/var/cache/wg-guard/core"
	ManagedAWGBinaryPath = "/usr/local/bin/awg"
	ManagedAWGBuildPath  = CoreCacheDir + "/awg-2026-09/tools/src/wg"
	coreRevisionMarker   = ".wg-guard-revision"
	coreInstalledMarker  = ".wg-guard-installed"
)

func coreCheckoutPath(b CoreBundle, component string) string {
	return path.Join(CoreCacheDir, b.ID, component)
}

func toolsInstalledMarker(b CoreBundle) string {
	return path.Join(coreCheckoutPath(b, "tools"), coreInstalledMarker)
}

func kernelInstalledMarker(b CoreBundle) string {
	return path.Join(coreCheckoutPath(b, "kernel"), coreInstalledMarker)
}

func sourceInstalled(h Host, marker, commit string) bool {
	raw, err := h.ReadFile(marker)
	return err == nil && strings.TrimSpace(string(raw)) == commit
}

func ensurePinnedCheckout(ctx context.Context, h Host, b CoreBundle, component, repository, version, commit string) (string, error) {
	destination := coreCheckoutPath(b, component)
	revisionMarker := path.Join(destination, coreRevisionMarker)
	if sourceInstalled(h, revisionMarker, commit) {
		observed, err := h.Output(ctx, []string{"git", "-C", destination, "rev-parse", "HEAD"}, 15*time.Second)
		if err == nil && strings.TrimSpace(observed) == commit {
			if err := h.Run(ctx, []string{"git", "-C", destination, "diff", "--quiet", commit, "--"}, 15*time.Second); err == nil {
				return destination, nil
			}
		}
	}

	staging := destination + ".staging"
	if err := h.RemoveAll(staging); err != nil {
		return "", err
	}
	if err := h.MkdirAll(path.Dir(destination), 0o750); err != nil {
		return "", err
	}
	clone := []string{"git", "clone", "--quiet", "--depth", "1", "--branch", version, "--single-branch", repository, staging}
	if err := h.Run(ctx, clone, longTimeout); err != nil {
		return "", fmt.Errorf("install: download reviewed %s source: %w", component, err)
	}
	observed, err := h.Output(ctx, []string{"git", "-C", staging, "rev-parse", "HEAD"}, 15*time.Second)
	if err != nil || strings.TrimSpace(observed) != commit {
		_ = h.RemoveAll(staging)
		return "", fmt.Errorf("install: reviewed %s source revision mismatch", component)
	}
	if err := h.Run(ctx, []string{"git", "-C", staging, "diff", "--quiet", commit, "--"}, 15*time.Second); err != nil {
		_ = h.RemoveAll(staging)
		return "", fmt.Errorf("install: reviewed %s source is not clean", component)
	}
	if err := h.RemoveAll(destination); err != nil {
		_ = h.RemoveAll(staging)
		return "", err
	}
	if err := h.Rename(staging, destination); err != nil {
		_ = h.RemoveAll(staging)
		return "", err
	}
	if err := h.WriteFile(revisionMarker, []byte(commit+"\n"), 0o600); err != nil {
		return "", err
	}
	return destination, nil
}

func ensurePinnedTools(ctx context.Context, h Host, b CoreBundle) error {
	if sourceInstalled(h, toolsInstalledMarker(b), b.ToolsCommit) {
		if _, err := h.Stat(ManagedAWGBinaryPath); err == nil {
			if raw, outErr := h.Output(ctx, []string{"awg", "--version"}, 10*time.Second); outErr == nil && strings.Contains(raw, b.ToolsVersion) {
				return nil
			}
		}
	}
	if _, err := h.Stat(ManagedAWGBinaryPath); err == nil && !sourceInstalled(h, toolsInstalledMarker(b), b.ToolsCommit) {
		return fmt.Errorf("install: %s already exists and is not owned by this reviewed core", ManagedAWGBinaryPath)
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	source, err := ensurePinnedCheckout(ctx, h, b, "tools", b.ToolsRepository, b.ToolsVersion, b.ToolsCommit)
	if err != nil {
		return err
	}
	if err := h.Run(ctx, []string{"make", "-C", path.Join(source, "src"), "clean"}, time.Minute); err != nil {
		return fmt.Errorf("install: clean reviewed AWG tools build: %w", err)
	}
	if err := h.Run(ctx, []string{"make", "-C", path.Join(source, "src")}, longTimeout); err != nil {
		return fmt.Errorf("install: build reviewed AWG tools: %w", err)
	}
	built := path.Join(source, "src", "wg")
	if err := h.CopyFile(built, ManagedAWGBinaryPath, 0o755); err != nil {
		return fmt.Errorf("install: install reviewed AWG tool: %w", err)
	}
	if raw, err := h.Output(ctx, []string{"awg", "--version"}, 10*time.Second); err != nil || !strings.Contains(raw, b.ToolsVersion) {
		_ = h.Remove(ManagedAWGBinaryPath)
		return fmt.Errorf("install: reviewed AWG tool failed version verification")
	}
	return h.WriteFile(toolsInstalledMarker(b), []byte(b.ToolsCommit+"\n"), 0o600)
}

func ensurePinnedKernel(ctx context.Context, h Host, kernel string, b CoreBundle) error {
	if sourceInstalled(h, kernelInstalledMarker(b), b.KernelCommit) && dkmsInstalled(ctx, h, kernel, b.KernelDKMSVersion) {
		return nil
	}
	if raw, err := h.Output(ctx, []string{"dkms", "status", "-m", "amneziawg", "-v", b.KernelDKMSVersion}, 15*time.Second); err == nil && strings.TrimSpace(raw) != "" {
		if err := h.Run(ctx, []string{"dkms", "remove", "-m", "amneziawg", "-v", b.KernelDKMSVersion, "--all"}, longTimeout); err != nil {
			return fmt.Errorf("install: remove incomplete reviewed AWG module: %w", err)
		}
	}

	source, err := ensurePinnedCheckout(ctx, h, b, "kernel", b.KernelRepository, b.KernelVersion, b.KernelCommit)
	if err != nil {
		return err
	}
	dkmsSource := path.Join("/usr/src", "amneziawg-"+b.KernelDKMSVersion)
	if err := h.RemoveAll(dkmsSource); err != nil {
		return err
	}
	makeArgs := []string{"make", "-C", path.Join(source, "src"), "WIREGUARD_VERSION=" + b.KernelDKMSVersion, "DKMSDIR=" + dkmsSource, "dkms-install"}
	if err := h.Run(ctx, makeArgs, longTimeout); err != nil {
		return fmt.Errorf("install: prepare reviewed AWG DKMS source: %w", err)
	}
	dkmsConfig := fmt.Sprintf("PACKAGE_NAME=\"amneziawg\"\nPACKAGE_VERSION=\"%s\"\nAUTOINSTALL=yes\nREMAKE_INITRD=yes\n\nBUILT_MODULE_NAME=\"amneziawg\"\nDEST_MODULE_LOCATION=\"/kernel/net\"\n", b.KernelDKMSVersion)
	if err := h.WriteFile(path.Join(dkmsSource, "dkms.conf"), []byte(dkmsConfig), 0o644); err != nil {
		return err
	}
	if err := h.Run(ctx, []string{"dkms", "add", "-m", "amneziawg", "-v", b.KernelDKMSVersion}, time.Minute); err != nil {
		return fmt.Errorf("install: register reviewed AWG module with DKMS: %w", err)
	}
	if err := h.Run(ctx, []string{"dkms", "install", "-m", "amneziawg", "-v", b.KernelDKMSVersion, "-k", kernel}, longTimeout); err != nil {
		return fmt.Errorf("install: build reviewed AWG module for kernel %s: %w", kernel, err)
	}
	if !dkmsInstalled(ctx, h, kernel, b.KernelDKMSVersion) {
		return fmt.Errorf("install: reviewed AWG DKMS build did not report installed")
	}
	return h.WriteFile(kernelInstalledMarker(b), []byte(b.KernelCommit+"\n"), 0o600)
}

func dkmsInstalled(ctx context.Context, h Host, kernel, version string) bool {
	raw, err := h.Output(ctx, []string{"dkms", "status", "-m", "amneziawg", "-v", version, "-k", kernel}, 15*time.Second)
	if err != nil {
		return false
	}
	line := strings.ToLower(strings.TrimSpace(raw))
	return strings.Contains(line, "amneziawg/"+strings.ToLower(version)) && strings.Contains(line, kernel) && strings.Contains(line, ": installed")
}

func purgePinnedSourceCore(ctx context.Context, h Host, r CoreReport) error {
	b, err := SelectCore(r.Requested.ID)
	if err != nil || b != r.Requested || b.Source != coreSourceGitHub {
		return fmt.Errorf("uninstall: recorded reviewed core source is invalid")
	}
	if r.ToolsSource == coreSourceGitHub {
		if !sourceInstalled(h, toolsInstalledMarker(b), b.ToolsCommit) {
			return fmt.Errorf("uninstall: managed AWG tool ownership marker is missing")
		}
		if err := h.Remove(ManagedAWGBinaryPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	if r.KernelSource == coreSourceGitHub {
		if !sourceInstalled(h, kernelInstalledMarker(b), b.KernelCommit) || r.KernelDKMS != b.KernelDKMSVersion {
			return fmt.Errorf("uninstall: managed AWG module ownership marker is missing")
		}
		if err := h.Run(ctx, []string{"dkms", "remove", "-m", "amneziawg", "-v", b.KernelDKMSVersion, "--all"}, longTimeout); err != nil {
			return fmt.Errorf("uninstall: remove reviewed AWG module: %w", err)
		}
		if err := h.RemoveAll(path.Join("/usr/src", "amneziawg-"+b.KernelDKMSVersion)); err != nil {
			return err
		}
	}
	return h.RemoveAll(path.Join(CoreCacheDir, b.ID))
}
