package install

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"time"
)

// The concrete deployment adapter executes already validated state while the
// caller owns the one host lifecycle lock. It never starts a second coordinator.
func startService(ctx context.Context, h Host, st *State) error {

	return runQuiet(ctx, h, []string{"docker", "compose", "-f", ComposePth, "up", "-d", "--pull", "never"}, longTimeout)

}
func stopService(ctx context.Context, h Host, st *State) error {

	if _, err := h.Stat(ComposePth); !errors.Is(err, fs.ErrNotExist) {
		if err != nil {
			return err
		}
		if err := runQuiet(ctx, h, []string{"docker", "compose", "-f", ComposePth, "down"}, longTimeout); err != nil {
			return err
		}
	}
	raw, err := h.Output(ctx, []string{"docker", "ps", "--filter", "name=^/" + Container + "$", "--format", "{{.ID}}"}, 30*time.Second)
	if err != nil {
		return err
	}
	if strings.TrimSpace(raw) != "" {
		return terminalError("install.error.stop")
	}
	return nil

}
