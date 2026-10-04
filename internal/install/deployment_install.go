package install

import (
	"context"
	"fmt"
	"io"
	"time"
)

// Deployment installation is called by the shared lifecycle coordinator after
// admission, acquisition and ownership checks. These adapters own no lock/journal.
func installDocker(ctx context.Context, h Host, p Plan, st *State, out io.Writer, beforeStart func(context.Context, Host, Plan, *State) error) error {
	step(out, "Docker preflight")
	if _, err := h.LookPath("docker"); err != nil {
		return fmt.Errorf("install: docker not found — install docker first (https://docs.docker.com/engine/install/) or use --mode native")
	}
	if err := runQuiet(ctx, h, []string{"docker", "compose", "version"}, 30*time.Second); err != nil {
		return fmt.Errorf("install: docker compose plugin missing (%v) — install docker-compose-plugin", err)
	}

	step(out, "Host CLI (shim)")
	self, err := h.SelfExe()
	if st.Current != nil {
		self = st.Current.Binary
		err = nil
	}
	if err != nil {
		return fmt.Errorf("install: locate running binary: %w", err)
	}
	if err := h.CopyFile(self, BinPath, 0o755); err != nil {
		return fmt.Errorf("install: install host CLI to %s: %w", BinPath, err)
	}
	st.BinPath = BinPath
	progress(out, "shim", self, BinPath)

	step(out, "Compose project")
	compose := RenderCompose(p)
	if err := h.WriteFile(ComposePth, []byte(compose), 0o644); err != nil {
		return fmt.Errorf("install: write compose: %w", err)
	}
	st.ComposePath = ComposePth
	st.Image = p.Image
	progress(out, "compose", ComposePth, p.Image)

	// Runtime settings (wizard choices) seed through the installed CLI while
	// the state file still doesn't exist — see seedSettings for why.
	if err := seedSettings(ctx, h, p, out); err != nil {
		return err
	}

	step(out, "Starting container")
	if beforeStart != nil {
		if err := beforeStart(ctx, h, p, st); err != nil {
			return err
		}
	}
	if err := runQuiet(ctx, h, []string{"docker", "compose", "-f", ComposePth, "up", "-d"}, longTimeout); err != nil {
		return fmt.Errorf("install: docker compose up: %w", err)
	}
	return nil
}

// installNative installs the binary + unit and starts the service.
func installNative(ctx context.Context, h Host, p Plan, st *State, out io.Writer, beforeStart func(context.Context, Host, Plan, *State) error) error {
	step(out, "systemd preflight")
	if _, err := h.LookPath("systemctl"); err != nil {
		return fmt.Errorf("install: systemctl not found — native mode needs systemd")
	}

	step(out, "Binary")
	self, err := h.SelfExe()
	if st.Current != nil {
		self = st.Current.Binary
		err = nil
	}
	if err != nil {
		return fmt.Errorf("install: locate running binary: %w", err)
	}
	if err := h.CopyFile(self, BinPath, 0o755); err != nil {
		return fmt.Errorf("install: install binary to %s: %w", BinPath, err)
	}
	st.BinPath = BinPath
	progressUI(out).Field("", self+" → "+BinPath)

	// Runtime settings (wizard choices) seed through the just-installed
	// binary before the service starts — see seedSettings for why.
	if err := seedSettings(ctx, h, p, out); err != nil {
		return err
	}

	step(out, "Systemd unit")
	if err := h.MkdirAll(JournalRetentionDir, 0o755); err != nil {
		return fmt.Errorf("install: create journal policy directory: %w", err)
	}
	if err := h.WriteFile(JournalRetentionPath, []byte(RenderJournalRetention()), 0o644); err != nil {
		return fmt.Errorf("install: write journal policy: %w", err)
	}
	st.ExtraFiles = addUnique(st.ExtraFiles, JournalRetentionPath)
	if err := h.WriteFile(UnitPath, []byte(RenderUnit(p)), 0o644); err != nil {
		return fmt.Errorf("install: write unit: %w", err)
	}
	st.UnitPath = UnitPath
	if err := runQuiet(ctx, h, []string{"systemctl", "daemon-reload"}, 30*time.Second); err != nil {
		return fmt.Errorf("install: daemon-reload: %w", err)
	}
	if err := runQuiet(ctx, h, []string{"systemctl", "try-restart", "systemd-journald@wg-guard.service"}, 30*time.Second); err != nil {
		return fmt.Errorf("install: reload journal namespace: %w", err)
	}
	if beforeStart != nil {
		if err := beforeStart(ctx, h, p, st); err != nil {
			return err
		}
	}
	if err := runQuiet(ctx, h, []string{"systemctl", "enable", "--now", "wg-guard"}, 60*time.Second); err != nil {
		return fmt.Errorf("install: enable service: %w", err)
	}
	progress(out, "started")
	return nil
}
