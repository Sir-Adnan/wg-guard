package install

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// Update performs one explicit, operator-initiated update (never automatic):
// pre-upgrade backup → swap (image pull or binary replace) → restart → health
// check → automatic rollback to the previous artifact when the health check
// fails. The pre-upgrade archive is the durable rollback for state.
func Update(ctx context.Context, h Host, o UpdateOptions) error {
	out := o.Stdout
	st, err := LoadState(h)
	if err != nil {
		return err
	}
	if st == nil {
		return fmt.Errorf("update: no install state at %s — run 'wg-guard install' first", StatePath)
	}

	if !o.SkipBackup {
		step(out, "Pre-upgrade backup")
		if err := runBackup(ctx, h, st); err != nil {
			return fmt.Errorf("update: pre-upgrade backup failed (nothing changed): %w", err)
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

// runBackup invokes backup create in the owning environment: inside the
// container in docker mode (the volume's DB owner), locally in native mode.
func runBackup(ctx context.Context, h Host, st *State) error {
	if st.Mode == ModeDocker {
		return h.Run(ctx, []string{
			"docker", "exec", Container, BinPath,
			"backup", "create", "--reason", "pre-upgrade",
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

	step(out, "Pulling image")
	if err := h.Run(ctx, []string{"docker", "compose", "-f", st.ComposePath, "pull"}, longTimeout); err != nil {
		return fmt.Errorf("update: compose pull: %w", err)
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

// patchStateImage rewrites the state file's image field (docker mode).
func patchStateImage(h Host, st *State, image string) error {
	st.Image = image
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return h.WriteFile(StatePath, append(data, '\n'), 0o644)
}
