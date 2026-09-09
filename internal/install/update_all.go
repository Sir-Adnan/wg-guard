package install

import (
	"context"
	"fmt"
	"io"

	"github.com/Sir-Adnan/wg-guard/internal/distribution"
)

type FullUpdateOptions struct {
	Build       distribution.Build
	StageParent string
	Core        string
	Stdout      io.Writer
}

// UpdateAll is an ordered coordinator, not one oversized transaction. Each
// component retains its own durable safety boundary: verified manager cache,
// backup/health-checked panel update, then catalogued core maintenance. A
// failure stops later components and reports the already-completed boundary.
func UpdateAll(ctx context.Context, h Host, o FullUpdateOptions) error {
	if err := CheckLifecycleReady(h); err != nil {
		return err
	}
	st, err := LoadState(h)
	if err != nil {
		return err
	}
	if st == nil {
		return terminalError("install.error.no_state")
	}
	bundle, err := SelectCore(o.Core)
	if err != nil {
		return err
	}
	if err = UpdateManager(ctx, h, ManagerUpdateOptions{Build: o.Build, Stdout: o.Stdout}); err != nil {
		return fmt.Errorf("update all: manager: %w", err)
	}
	image := ""
	localImage := false
	if st.Mode == ModeDocker {
		image, err = BuildRuntimeImage(ctx, h, o.Build, bundle, o.StageParent)
		if err != nil {
			return fmt.Errorf("update all: manager updated; Docker runtime not changed: %w", err)
		}
		localImage = true
	}
	if err = Update(ctx, h, UpdateOptions{
		Build: o.Build, BinaryPath: o.Build.BinaryPath, Image: image,
		LocalImage: localImage, Stdout: o.Stdout,
	}); err != nil {
		return fmt.Errorf("update all: manager updated; panel recovered or requires its recorded recovery: %w", err)
	}
	if _, err = SwitchCore(ctx, h, CoreSwitchOptions{Selector: bundle.ID, ConfirmImpact: true, Stdout: o.Stdout}); err != nil {
		return fmt.Errorf("update all: manager and panel updated; core needs attention: %w", err)
	}
	return nil
}
