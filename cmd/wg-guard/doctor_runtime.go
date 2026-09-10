package main

import (
	"context"
	"fmt"

	"github.com/Sir-Adnan/wg-guard/internal/install"
	"github.com/Sir-Adnan/wg-guard/internal/subprocess"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel/amneziawg"
)

// dockerExecRunner keeps read-only AWG inspection in the environment that
// owns the pinned tools. The host runner remains separate for kernel,
// firewall, sysctl, shaping and exposure checks.
type dockerExecRunner struct {
	host subprocess.Runner
}

func (r dockerExecRunner) Run(ctx context.Context, argv []string) (subprocess.Result, error) {
	if len(argv) == 0 {
		return subprocess.Result{}, fmt.Errorf("docker AWG inspection: empty argv")
	}
	command := make([]string, 0, len(argv)+4)
	command = append(command, "docker", "exec", "-i", install.Container)
	command = append(command, argv...)
	return r.host.Run(ctx, command)
}

func newDoctorInspector(state *install.State, host subprocess.Runner) *amneziawg.Backend {
	if state != nil && state.Mode == install.ModeDocker {
		return amneziawg.New(dockerExecRunner{host: host})
	}
	return amneziawg.New(host)
}

// prepareDoctorFix maps Docker repair onto the managed lifecycle restart.
// Container startup already runs the canonical boot reconciliation with its
// pinned AWG tools; the subsequent read-only doctor pass verifies the result.
// Native mode retains doctor's direct offline repair path.
func prepareDoctorFix(ctx context.Context, state *install.State, configPath string, fix bool, restart func(context.Context) error) (directFix bool, summary string, err error) {
	if !fix || state == nil || state.Mode != install.ModeDocker || state.ConfigPath != configPath {
		return fix, "", nil
	}
	if err := restart(ctx); err != nil {
		return false, "", fmt.Errorf("Docker repair restart: %w", err)
	}
	return false, "restarted Docker node; startup reconciliation completed and passed its health gate", nil
}
