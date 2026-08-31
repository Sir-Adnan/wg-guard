package install

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// Update performs one explicit, operator-initiated update (never automatic):
// pre-upgrade backup → swap (image pull or binary replace) → restart → health
// check → automatic rollback to the previous artifact when the health check
// fails. The pre-upgrade archive is the durable rollback for state.
//
// If an update is interrupted (killed mid-flight, host reboot), the compose
// file or binary may reference a bad artifact while the state file still
// records the last known-good one: `wg-guard update --rollback` re-deploys
// that recorded image/binary without touching backups.
func Update(ctx context.Context, h Host, o UpdateOptions) error {
	out := o.Stdout
	st, err := LoadState(h)
	if err != nil {
		return err
	}
	if st == nil {
		return fmt.Errorf("update: no install state at %s — run 'wg-guard install' first", StatePath)
	}

	if o.Rollback {
		step(out, "Rolling back to the last healthy artifact")
		switch st.Mode {
		case ModeDocker:
			return rollbackDocker(ctx, h, st, out)
		case ModeNative:
			return rollbackNative(ctx, h, st, out)
		}
		return fmt.Errorf("update: unknown mode %q", st.Mode)
	}

	if !o.SkipBackup {
		step(out, "Pre-upgrade backup")
		if err := runBackup(ctx, h, st); err != nil {
			return fmt.Errorf("update: pre-upgrade backup failed (nothing changed): %w"+
				"\n  if the node is down, recover with: wg-guard update --rollback", err)
		}
		fmt.Fprintln(out, "  archive written to the node's backup sink")
	}

	switch st.Mode {
	case ModeDocker:
		return updateDocker(ctx, h, st, o, out)
	case ModeNative:
		return updateNative(ctx, h, st, o, out)
	}
	return fmt.Errorf("update: unknown mode %q", st.Mode)
}

// rollbackDocker re-deploys the state-recorded (last health-checked) image.
func rollbackDocker(ctx context.Context, h Host, st *State, out io.Writer) error {
	if st.Image == "" {
		return fmt.Errorf("rollback: state records no image — restore %s manually", st.ComposePath)
	}
	compose, err := h.ReadFile(st.ComposePath)
	if err != nil {
		return fmt.Errorf("rollback: read compose: %w", err)
	}
	current := imageFromCompose(string(compose))
	if current == st.Image {
		return fmt.Errorf("rollback: compose already references %s", current)
	}
	fixed := strings.Replace(string(compose), "image: "+current, "image: "+st.Image, 1)
	if fixed == string(compose) {
		return fmt.Errorf("rollback: could not switch image reference %s -> %s", current, st.Image)
	}
	if err := h.WriteFile(st.ComposePath, []byte(fixed), 0o644); err != nil {
		return fmt.Errorf("rollback: write compose: %w", err)
	}
	if err := h.Run(ctx, []string{"docker", "compose", "-f", st.ComposePath, "up", "-d"}, longTimeout); err != nil {
		return fmt.Errorf("rollback: compose up: %w", err)
	}
	if err := waitHealthyRecorded(ctx, h, st, updateHealthWindow, out); err != nil {
		return fmt.Errorf("rollback: node still unhealthy after redeploying %s: %w", st.Image, err)
	}
	// The last-known-good record is still correct; nothing to patch.
	fmt.Fprintf(out, "  node healthy on image %s\n", st.Image)
	return nil
}

// rollbackNative restores the <bin>.pre-update copy when present.
func rollbackNative(ctx context.Context, h Host, st *State, out io.Writer) error {
	keep := st.BinPath + ".pre-update"
	if _, err := h.Stat(keep); err != nil {
		return fmt.Errorf("rollback: no %s — reinstall the previous binary manually", keep)
	}
	if err := h.CopyFile(keep, st.BinPath, 0o755); err != nil {
		return fmt.Errorf("rollback: restore binary: %w", err)
	}
	if err := h.Run(ctx, []string{"systemctl", "restart", "wg-guard"}, 90*time.Second); err != nil {
		return fmt.Errorf("rollback: restart: %w", err)
	}
	if err := waitHealthyRecorded(ctx, h, st, updateHealthWindow, out); err != nil {
		return fmt.Errorf("rollback: node still unhealthy: %w", err)
	}
	fmt.Fprintln(out, "  node healthy on the restored binary")
	return nil
}

// runBackup invokes backup create in the owning environment: inside the
// container in docker mode (the volume's DB owner), locally in native mode.
// Older images may predate the -reason flag; a plain retry keeps the
// pre-upgrade backup version-independent.
func runBackup(ctx context.Context, h Host, st *State) error {
	if st.Mode == ModeDocker {
		if err := h.Run(ctx, []string{
			"docker", "exec", Container, BinPath,
			"backup", "create", "--reason", "pre-upgrade",
		}, 5*time.Minute); err == nil {
			return nil
		}
		return h.Run(ctx, []string{
			"docker", "exec", Container, BinPath, "backup", "create",
		}, 5*time.Minute)
	}
	return h.Run(ctx, []string{
		BinPath, "backup", "create", "--reason", "pre-upgrade",
	}, 5*time.Minute)
}

// updateDocker pulls the (possibly new) image and recreates the container.
// Rollback re-renders the compose project with the previous image reference
// and brings it back up; the health check decides.
func updateDocker(ctx context.Context, h Host, st *State, o UpdateOptions, out io.Writer) error {
	oldCompose, err := h.ReadFile(st.ComposePath)
	if err != nil {
		return fmt.Errorf("update: read compose: %w", err)
	}
	image := o.Image
	if image == "" {
		image = st.Image
	}
	if image == "" {
		return fmt.Errorf("update: compose has no recorded image and --image was not given")
	}
	// The compose file is the source of truth for what runs (the state file
	// is a record; reconcile from it if they ever drift).
	current := imageFromCompose(string(oldCompose))
	if current == "" {
		return fmt.Errorf("update: %s has no image reference — edit it or reinstall", st.ComposePath)
	}
	if current == image {
		return fmt.Errorf("update: already on image %s (pass --image to change it)", image)
	}
	// Point the compose project at the new image BEFORE pulling: `up -d`
	// recreates the container only when the rendered config changed.
	newCompose := strings.Replace(string(oldCompose), "image: "+current, "image: "+image, 1)
	if newCompose == string(oldCompose) {
		return fmt.Errorf("update: could not switch image reference %s -> %s in %s", current, image, st.ComposePath)
	}

	step(out, "Switching image reference")
	if err := h.WriteFile(st.ComposePath, []byte(newCompose), 0o644); err != nil {
		return fmt.Errorf("update: write compose: %w", err)
	}

	step(out, "Pulling image")
	// Best-effort pull: a locally-built image (no registry) cannot be pulled;
	// `up -d` below still resolves it. A real registry failure surfaces at
	// up -d and the health check/rollback handles it.
	if err := h.Run(ctx, []string{"docker", "compose", "-f", st.ComposePath, "pull"}, longTimeout); err != nil {
		fmt.Fprintf(out, "  WARNING: pull failed (%v) — continuing with the locally available image\n", err)
	}

	step(out, "Recreating container")
	if err := h.Run(ctx, []string{"docker", "compose", "-f", st.ComposePath, "up", "-d"}, longTimeout); err != nil {
		return fmt.Errorf("update: compose up: %w", err)
	}

	step(out, "Health check")
	if err := waitHealthyRecorded(ctx, h, st, updateHealthWindow, out); err != nil {
		fmt.Fprintf(out, "  update failed (%v) — rolling back\n", err)
		if err := h.WriteFile(st.ComposePath, oldCompose, 0o644); err != nil {
			return fmt.Errorf("update: rollback compose write: %w", err)
		}
		if err := h.Run(ctx, []string{"docker", "compose", "-f", st.ComposePath, "up", "-d"}, longTimeout); err != nil {
			return fmt.Errorf("update: ROLLBACK FAILED — inspect 'docker compose -f %s ps' manually", st.ComposePath)
		}
		if err := waitHealthyRecorded(ctx, h, st, updateHealthWindow, out); err != nil {
			return fmt.Errorf("update: rolled back to %s but the node is unhealthy: %w", st.Image, err)
		}
		return fmt.Errorf("update: rolled back to %s (pre-upgrade backup is available)", st.Image)
	}

	// Persist the new reference for the next update/rollback decision.
	if st.Image != image {
		if err := patchStateImage(h, st, image); err != nil {
			fmt.Fprintf(out, "  WARNING: state file not updated with the new image: %v\n", err)
		}
	}
	fmt.Fprintf(out, "  node healthy on image %s\n", image)
	return nil
}

// updateNative replaces the binary (keeping the previous one as
// <bin>.pre-update), restarts and health checks; on failure the previous
// binary is restored and the service restarted again.
func updateNative(ctx context.Context, h Host, st *State, o UpdateOptions, out io.Writer) error {
	if o.BinaryPath == "" {
		return fmt.Errorf("update: native mode needs --binary PATH (the staged new wg-guard binary); " +
			"downloads are never fetched automatically")
	}
	if _, err := h.Stat(o.BinaryPath); err != nil {
		return fmt.Errorf("update: staged binary %s: %w", o.BinaryPath, err)
	}

	step(out, "Staging binary")
	keep := st.BinPath + ".pre-update"
	if err := h.CopyFile(st.BinPath, keep, 0o755); err != nil {
		return fmt.Errorf("update: keep previous binary: %w", err)
	}
	if err := h.CopyFile(o.BinaryPath, st.BinPath+".new", 0o755); err != nil {
		return fmt.Errorf("update: stage new binary: %w", err)
	}

	apply := func() error {
		if err := h.Rename(st.BinPath+".new", st.BinPath); err != nil {
			return err
		}
		return h.Run(ctx, []string{"systemctl", "restart", "wg-guard"}, 90*time.Second)
	}

	step(out, "Applying + restart")
	if err := apply(); err != nil {
		return fmt.Errorf("update: apply: %w", err)
	}

	step(out, "Health check")
	if err := waitHealthyRecorded(ctx, h, st, updateHealthWindow, out); err != nil {
		fmt.Fprintf(out, "  update failed (%v) — restoring previous binary\n", err)
		if err := h.CopyFile(keep, st.BinPath, 0o755); err != nil {
			return fmt.Errorf("update: ROLLBACK FAILED — previous binary kept at %s", keep)
		}
		if err := h.Run(ctx, []string{"systemctl", "restart", "wg-guard"}, 90*time.Second); err != nil {
			return fmt.Errorf("update: rollback restart failed: %w", err)
		}
		if err := waitHealthyRecorded(ctx, h, st, updateHealthWindow, out); err != nil {
			return fmt.Errorf("update: rolled back but the node is unhealthy: %w", err)
		}
		return fmt.Errorf("update: rolled back to the previous binary (pre-upgrade backup is available)")
	}
	fmt.Fprintf(out, "  node healthy (previous binary kept at %s)\n", keep)
	return nil
}

// waitHealthyRecorded is waitHealthy with progress dots and mode-aware probe
// resolution from the state file.
func waitHealthyRecorded(ctx context.Context, h Host, st *State, within time.Duration, out io.Writer) error {
	p := Plan{Mode: st.Mode, DataDir: st.DataDir}
	// Reconstruct the plan's TLS posture from the live boot config.
	cfg, err := ReadBootConfig(h, st.ConfigPath)
	if err != nil {
		return err
	}
	p.TLSMode = cfg.TLS.Mode
	p.Domain = cfg.TLS.Domain
	p.PanelPort = portOf(cfg.HTTPListen)
	p.ACMEHTTPPort = cfg.TLS.ACMEHTTPPort
	if p.ACMEHTTPPort == 0 {
		p.ACMEHTTPPort = 80
	}
	return waitHealthy(ctx, h, p, within)
}

// imageFromCompose extracts the service image reference from a compose file
// (first "image:" line, trimmed).
func imageFromCompose(content string) string {
	for _, line := range strings.Split(content, "\n") {
		t := strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(t, "image:"); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// patchStateImage rewrites the state file's image field (docker mode).
func patchStateImage(h Host, st *State, image string) error {
	st.Image = image
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return h.WriteFile(StatePath, append(data, '\n'), 0o644)
}
