package install

import (
	"context"
	"io"
	"time"
)

// RuntimeBuilderPackage is Ubuntu's BuildKit plugin for the docker.io engine.
const RuntimeBuilderPackage = "docker-buildx"

// EnsureRuntimeBuilder provisions BuildKit before a local runtime build.
// Docker deprecated its legacy builder, which also runs the recipe's independent
// build stages one after another. Only an explicitly selected development
// source builds the runtime locally, so release installs record no extra package.
//
// It is best effort and never fails the install: without the plugin the legacy
// builder still produces the same recipe while Docker ships it. The plugin is
// installed only beside an Ubuntu docker.io engine, never into another vendor's
// engine, and it is recorded as installer-owned for managed removal.
func EnsureRuntimeBuilder(ctx context.Context, h Host, platform PlatformReport, policy PrerequisitePolicy, st *State, out io.Writer) {
	if dockerBuildxAvailable(ctx, h) {
		return
	}
	if !automaticPackages(platform, policy) || installedPackage(ctx, h, "docker.io") == "" ||
		!packageAvailable(ctx, h, RuntimeBuilderPackage, "") {
		progress(out, "runtime_legacy_builder")
		return
	}
	err := runQuiet(ctx, h, []string{"apt-get", "install", "-y", "--no-install-recommends", "--no-upgrade", "--no-remove", RuntimeBuilderPackage}, longTimeout)
	if installedPackage(ctx, h, RuntimeBuilderPackage) != "" {
		st.PackagesInstalled = addUnique(st.PackagesInstalled, RuntimeBuilderPackage)
	}
	if err != nil || !dockerBuildxAvailable(ctx, h) {
		progress(out, "runtime_legacy_builder")
	}
}

// dockerBuildxAvailable is a silent probe; a missing plugin is not a failure.
func dockerBuildxAvailable(ctx context.Context, h Host) bool {
	_, err := h.Output(ctx, []string{"docker", "buildx", "version"}, 30*time.Second)
	return err == nil
}
