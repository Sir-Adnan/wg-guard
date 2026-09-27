package amneziawg

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/subprocess"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel"
)

const userspaceSocketDir = "/var/run/amneziawg" // pinned source ipc/uapi_unix.go
var userspaceName = regexp.MustCompile(`^awg[0-9]+$`)

type userspaceManager struct {
	mu        sync.Mutex
	run       subprocess.Runner
	processes map[string]*subprocess.ManagedProcess
}

func newUserspaceManager(run subprocess.Runner) *userspaceManager {
	return &userspaceManager{run: run, processes: make(map[string]*subprocess.ManagedProcess)}
}

func userspaceSocketActive(name string) (bool, error) {
	if !userspaceName.MatchString(name) || len(name) > 15 {
		return false, fmt.Errorf("amneziawg: invalid userspace interface name")
	}
	path := filepath.Join(userspaceSocketDir, name+".sock")
	st, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("amneziawg: inspect userspace socket: %w", err)
	}
	if st.Mode()&os.ModeSocket == 0 {
		return false, fmt.Errorf("amneziawg: userspace path is not a socket")
	}
	conn, err := net.DialTimeout("unix", path, 150*time.Millisecond)
	if err == nil {
		conn.Close()
		return true, nil
	}
	if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, fmt.Errorf("amneziawg: userspace socket probe: %w", err)
}

func (m *userspaceManager) start(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	version, err := m.run.Run(ctx, []string{"amneziawg-go", "--version"})
	if err != nil || !strings.Contains(string(version.Stdout), "v3.1.20260828") {
		return fmt.Errorf("pinned amneziawg-go v3.1.20260828 is required")
	}
	if p := m.processes[name]; p != nil && p.Alive() {
		return fmt.Errorf("userspace daemon already managed for %s", name)
	}
	delete(m.processes, name)
	active, err := userspaceSocketActive(name)
	if err != nil {
		return err
	}
	if active {
		return fmt.Errorf("active userspace daemon for %s is not owned by this node", name)
	}
	// The pinned daemon accepts --foreground IFACE and creates the TUN and UAPI
	// socket itself. One child is retained per managed interface.
	p, err := subprocess.StartManaged([]string{"amneziawg-go", "--foreground", name})
	if err != nil {
		return fmt.Errorf("start pinned userspace daemon: %w", err)
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		if !p.Alive() {
			return fmt.Errorf("userspace daemon exited before its socket became ready")
		}
		active, err = userspaceSocketActive(name)
		if err == nil && active {
			m.processes[name] = p
			return nil
		}
		if err != nil {
			_ = p.Stop()
			return err
		}
		select {
		case <-ctx.Done():
			_ = p.Stop()
			return ctx.Err()
		case <-deadline.C:
			_ = p.Stop()
			return fmt.Errorf("userspace daemon socket did not become ready")
		case <-tick.C:
		}
	}
}

func (m *userspaceManager) stop(name string) (bool, error) {
	m.mu.Lock()
	p := m.processes[name]
	delete(m.processes, name)
	m.mu.Unlock()
	if p == nil {
		return false, nil
	}
	return true, p.Stop()
}

func (m *userspaceManager) observed(name string) (string, error) {
	m.mu.Lock()
	p := m.processes[name]
	m.mu.Unlock()
	if p != nil {
		if !p.Alive() {
			return "", tunnel.ErrInterfaceNotFound
		}
		active, err := userspaceSocketActive(name)
		if err != nil {
			return "", err
		}
		if !active {
			_, _ = m.stop(name)
			return "", tunnel.ErrInterfaceNotFound
		}
		return "userspace", nil
	}
	active, err := userspaceSocketActive(name)
	if err != nil {
		return "", err
	}
	if active {
		return "", fmt.Errorf("amneziawg: userspace daemon for %s is not owned by this node", name)
	}
	return "kernel", nil
}

func (b *Backend) observedMode(name string) (string, error) {
	if b.userspace != nil {
		return b.userspace.observed(name)
	}
	active, err := userspaceSocketActive(name)
	if err != nil {
		return "", err
	}
	if active {
		return "", fmt.Errorf("amneziawg: userspace daemon for %s requires the managed node service", name)
	}
	return "kernel", nil
}

func (m *userspaceManager) needsRepair() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, p := range m.processes {
		if !p.Alive() {
			return true
		}
		active, err := userspaceSocketActive(name)
		if err != nil || !active {
			return true
		}
	}
	return false
}

func (m *userspaceManager) close() error {
	m.mu.Lock()
	processes := m.processes
	m.processes = make(map[string]*subprocess.ManagedProcess)
	m.mu.Unlock()
	var errs []error
	for _, p := range processes {
		errs = append(errs, p.Stop())
	}
	return errors.Join(errs...)
}
