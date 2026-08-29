package network

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// EnsureIPForwarding idempotently enables net.ipv4.ip_forward (docs/
// architecture/networking.md). It returns the value observed before the call
// (0 when it changed, 1 when already enabled) so the caller can persist it
// for uninstall restore; persisting stays the caller's job because the panel
// process owns the data directory, not this package.
func (l *Links) EnsureIPForwarding(ctx context.Context) (prev int, changed bool, err error) {
	res, rerr := l.Run.Run(ctx, []string{"sysctl", "-n", "net.ipv4.ip_forward"})
	if rerr != nil {
		return 0, false, fmt.Errorf("network: read net.ipv4.ip_forward: %w", rerr)
	}
	v, perr := strconv.Atoi(strings.TrimSpace(string(res.Stdout)))
	if perr != nil || (v != 0 && v != 1) {
		return 0, false, fmt.Errorf("network: unexpected net.ipv4.ip_forward value %q", strings.TrimSpace(string(res.Stdout)))
	}
	if v == 1 {
		return 1, false, nil
	}
	if _, err := l.run(ctx, []string{"sysctl", "-w", "net.ipv4.ip_forward=1"}); err != nil {
		return 0, false, err
	}
	return 0, true, nil
}
