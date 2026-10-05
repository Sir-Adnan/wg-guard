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
		return fmt.Errorf("install: docker not found — install docker first (https://docs.docker.com/engine/install/)")
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
	if err := runQuiet(ctx, h, []string{"docker", "compose", "-f", ComposePth, "up", "-d", "--pull", "never"}, longTimeout); err != nil {
		return fmt.Errorf("install: docker compose up: %w", err)
	}
	return nil
}
