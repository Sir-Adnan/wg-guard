package install

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	coreSourcePackage = "ubuntu-package"
	coreSourceGitHub  = "github-source"

	CoreCacheDir        = "/var/cache/wg-guard/core"
	coreRevisionMarker  = ".wg-guard-revision"
	coreInstalledMarker = ".wg-guard-installed"
)

func coreCheckoutPath(b CoreBundle, component string) string {
	return path.Join(CoreCacheDir, b.ID, component)
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
	clone := []string{"git", "-c", "advice.detachedHead=false", "clone", "--quiet", "--depth", "1", "--branch", version, "--single-branch", repository, staging}
	if err := runQuiet(ctx, h, clone, longTimeout); err != nil {
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

func ensurePinnedKernel(ctx context.Context, h Host, kernel string, b CoreBundle) error {
	if sourceInstalled(h, kernelInstalledMarker(b), b.KernelCommit) && dkmsInstalled(ctx, h, kernel, b.KernelDKMSVersion) {
		if err := ensureInstalledKernelBuilds(ctx, h, kernel, b); err != nil {
			return err
		}
		return retireSupersededSourceModules(ctx, h, b)
	}
	if raw, err := h.Output(ctx, []string{"dkms", "status", "-m", "amneziawg", "-v", b.KernelDKMSVersion}, 15*time.Second); err == nil && strings.TrimSpace(raw) != "" {
		if err := runQuiet(ctx, h, []string{"dkms", "remove", "-m", "amneziawg", "-v", b.KernelDKMSVersion, "--all"}, longTimeout); err != nil {
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
	if err := runQuiet(ctx, h, makeArgs, longTimeout); err != nil {
		return fmt.Errorf("install: prepare reviewed AWG DKMS source: %w", err)
	}
	if err := applyKernelSourcePatches(h, b, dkmsSource); err != nil {
		return err
	}
	dkmsConfig := fmt.Sprintf("PACKAGE_NAME=\"amneziawg\"\nPACKAGE_VERSION=\"%s\"\nAUTOINSTALL=yes\n\nBUILT_MODULE_NAME=\"amneziawg\"\nDEST_MODULE_LOCATION=\"/kernel/net\"\n", b.KernelDKMSVersion)
	if err := h.WriteFile(path.Join(dkmsSource, "dkms.conf"), []byte(dkmsConfig), 0o644); err != nil {
		return err
	}
	if err := runQuiet(ctx, h, []string{"dkms", "add", "-m", "amneziawg", "-v", b.KernelDKMSVersion}, time.Minute); err != nil {
		return fmt.Errorf("install: register reviewed AWG module with DKMS: %w", err)
	}
	if err := runQuiet(ctx, h, []string{"dkms", "install", "-m", "amneziawg", "-v", b.KernelDKMSVersion, "-k", kernel}, longTimeout); err != nil {
		return fmt.Errorf("install: build reviewed AWG module for kernel %s: %w", kernel, err)
	}
	if !dkmsInstalled(ctx, h, kernel, b.KernelDKMSVersion) {
		return fmt.Errorf("install: reviewed AWG DKMS build did not report installed")
	}
	if err := ensureInstalledKernelBuilds(ctx, h, kernel, b); err != nil {
		return err
	}
	if err := h.WriteFile(kernelInstalledMarker(b), []byte(b.KernelCommit+"\n"), 0o600); err != nil {
		return err
	}
	return retireSupersededSourceModules(ctx, h, b)
}

// retireSupersededSourceModules removes other catalogued WG-Guard source
// registrations once b is installed for every target kernel. A superseded
// AUTOINSTALL entry would otherwise be rebuilt by each kernel package hook and
// can fail there (an uncorrected source on Ubuntu 7.0.0-38). Foreign or
// package-owned DKMS modules are never touched.
func retireSupersededSourceModules(ctx context.Context, h Host, b CoreBundle) error {
	for _, other := range reviewedCoreBundles {
		if other.Source != coreSourceGitHub || other.KernelDKMSVersion == b.KernelDKMSVersion {
			continue
		}
		if raw, err := h.Output(ctx, []string{"dkms", "status", "-m", "amneziawg", "-v", other.KernelDKMSVersion}, 15*time.Second); err == nil && strings.TrimSpace(raw) != "" {
			if err := runQuiet(ctx, h, []string{"dkms", "remove", "-m", "amneziawg", "-v", other.KernelDKMSVersion, "--all"}, longTimeout); err != nil {
				return fmt.Errorf("install: retire superseded AWG module %s: %w", other.KernelDKMSVersion, err)
			}
		}
		if err := h.RemoveAll(path.Join("/usr/src", "amneziawg-"+other.KernelDKMSVersion)); err != nil {
			return err
		}
		if err := h.RemoveAll(path.Join(CoreCacheDir, other.ID)); err != nil {
			return err
		}
	}
	return nil
}

var installedKernelName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,127}$`)

// Header meta-packages may install the next boot kernel before the reviewed
// source is registered with DKMS. Cover those already bootable/header-ready
// kernels now; future package hooks retain AUTOINSTALL. Never build other modules.
func ensureInstalledKernelBuilds(ctx context.Context, h Host, running string, b CoreBundle) error {
	if !installedKernelName.MatchString(running) {
		return fmt.Errorf("install: invalid running kernel identity")
	}
	targets := []string{running}
	// Ubuntu's /lib is a usr-merge symlink; use its canonical root so the
	// managed host's no-symlink read guard remains intact.
	entries, err := h.ReadDir("/usr/lib/modules")
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("install: inspect installed kernels: %w", err)
	}
	if len(entries) > 32 {
		return fmt.Errorf("install: installed kernel inventory exceeds reviewed bound")
	}
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || name == running || !installedKernelName.MatchString(name) {
			continue
		}
		image, imageErr := h.Stat(path.Join("/boot", "vmlinuz-"+name))
		headers, headerErr := h.Stat(path.Join("/usr/lib/modules", name, "build", "Makefile"))
		if imageErr == nil && headerErr == nil && image.Mode().IsRegular() && headers.Mode().IsRegular() {
			targets = append(targets, name)
		}
	}
	sort.Strings(targets[1:])
	for _, kernel := range targets {
		if dkmsInstalled(ctx, h, kernel, b.KernelDKMSVersion) {
			continue
		}
		if err := runQuiet(ctx, h, []string{"dkms", "install", "-m", "amneziawg", "-v", b.KernelDKMSVersion, "-k", kernel}, longTimeout); err != nil {
			return fmt.Errorf("install: build reviewed AWG module for installed kernel %s: %w", kernel, err)
		}
		if !dkmsInstalled(ctx, h, kernel, b.KernelDKMSVersion) {
			return fmt.Errorf("install: reviewed AWG DKMS build did not report installed for kernel %s", kernel)
		}
	}
	return nil
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
	if r.ExternalModule {
		return nil
	}
	if r.KernelSource == coreSourceGitHub {
		if !sourceInstalled(h, kernelInstalledMarker(b), b.KernelCommit) || r.KernelDKMS != b.KernelDKMSVersion {
			return fmt.Errorf("uninstall: managed AWG module ownership marker is missing")
		}
		if err := runQuiet(ctx, h, []string{"dkms", "remove", "-m", "amneziawg", "-v", b.KernelDKMSVersion, "--all"}, longTimeout); err != nil {
			return fmt.Errorf("uninstall: remove reviewed AWG module: %w", err)
		}
		if err := h.RemoveAll(path.Join("/usr/src", "amneziawg-"+b.KernelDKMSVersion)); err != nil {
			return err
		}
	}
	return h.RemoveAll(path.Join(CoreCacheDir, b.ID))
}
