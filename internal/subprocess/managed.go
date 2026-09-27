package subprocess

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// ManagedProcess is a long-lived, argv-only helper owned by the node. Output
// is never collected: daemon diagnostics must not become config/secret logs.
type ManagedProcess struct {
	cmd  *exec.Cmd
	done chan struct{}
}

func StartManaged(argv []string) (*ManagedProcess, error) {
	if len(argv) == 0 {
		return nil, fmt.Errorf("subprocess: empty managed argv")
	}
	null, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return nil, fmt.Errorf("subprocess: open null sink: %w", err)
	}
	defer null.Close()
	cmd := exec.Command(argv[0], argv[1:]...) // explicit argv, never a shell
	cmd.Stdout, cmd.Stderr = null, null
	cmd.SysProcAttr = managedProcessAttrs()
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("subprocess: start %s: %w", argv[0], err)
	}
	p := &ManagedProcess{cmd: cmd, done: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
		close(p.done)
	}()
	return p, nil
}

func (p *ManagedProcess) Alive() bool {
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

func (p *ManagedProcess) Stop() error {
	if p == nil || !p.Alive() {
		return nil
	}
	if err := p.cmd.Process.Signal(os.Interrupt); err != nil && !errors.Is(err, os.ErrProcessDone) && p.Alive() {
		return fmt.Errorf("subprocess: signal managed process: %w", err)
	}
	select {
	case <-p.done:
		return nil
	case <-time.After(5 * time.Second):
		_ = p.cmd.Process.Kill()
		<-p.done
		return fmt.Errorf("subprocess: managed process did not stop gracefully")
	}
}
