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
