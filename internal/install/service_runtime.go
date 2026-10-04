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
	if st.Mode == ModeDocker {
		return runQuiet(ctx, h, []string{"docker", "compose", "-f", ComposePth, "up", "-d", "--pull", "never"}, longTimeout)
	}
	if err := runQuiet(ctx, h, []string{"systemctl", "daemon-reload"}, 30*time.Second); err != nil {
		return err
	}
	if err := runQuiet(ctx, h, []string{"systemctl", "try-restart", "systemd-journald@wg-guard.service"}, 30*time.Second); err != nil {
		return err
	}
	return runQuiet(ctx, h, []string{"systemctl", "restart", "wg-guard"}, 90*time.Second)
}
func stopService(ctx context.Context, h Host, st *State) error {
	if st.Mode == ModeDocker {
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
	_, err := stopNativeService(ctx, h)
	return err
}

type nativeUnitState struct{ load, active string }

func (s nativeUnitState) absent() bool  { return s.load == "not-found" && s.active == "inactive" }
func (s nativeUnitState) stopped() bool { return s.active == "inactive" || s.active == "failed" }

// File absence alone is insufficient: systemd can retain a loaded/running unit
// after its file was removed. Query both properties and reject incomplete or
// inconsistent observations, even if systemctl printed them before an error.
func inspectNativeUnit(ctx context.Context, h Host) (nativeUnitState, error) {
	raw, err := h.Output(ctx, []string{"systemctl", "show", "wg-guard.service", "--property=LoadState", "--property=ActiveState"}, 30*time.Second)
	if err != nil {
		return nativeUnitState{}, err
	}
	s := nativeUnitState{}
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || value == "" {
			return s, terminalError("install.error.stop")
		}
		switch key {
		case "LoadState":
			if s.load != "" {
				return s, terminalError("install.error.stop")
			}
			s.load = value
		case "ActiveState":
			if s.active != "" {
				return s, terminalError("install.error.stop")
			}
			s.active = value
		default:
			return s, terminalError("install.error.stop")
		}
	}
	switch s.load {
	case "loaded", "masked", "not-found":
	default:
		return s, terminalError("install.error.stop")
	}
	switch s.active {
	case "active", "reloading", "inactive", "failed", "activating", "deactivating", "maintenance":
	default:
		return s, terminalError("install.error.stop")
	}
	if s.load == "not-found" && !s.absent() {
		return s, terminalError("install.error.stop")
	}
	return s, nil
}

// The bool tells uninstall whether disable can be skipped for a confirmed
// absent unit. A genuine stop failure is never reclassified as absence.
func stopNativeService(ctx context.Context, h Host) (bool, error) {
	s, err := inspectNativeUnit(ctx, h)
	if err != nil {
		return false, err
	}
	if s.absent() {
		return true, nil
	}
	if err := runQuiet(ctx, h, []string{"systemctl", "stop", "wg-guard"}, 60*time.Second); err != nil {
		return false, err
	}
	s, err = inspectNativeUnit(ctx, h)
	if err != nil {
		return false, err
	}
	if !s.stopped() {
		return false, terminalError("install.error.stop")
	}
	return s.absent(), nil
}

// Recovery has its own deadline so cancellation cannot disable compensation.
