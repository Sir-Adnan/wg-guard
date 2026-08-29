// Package network owns the host-level state around tunnel links: link
// creation/deletion and addressing via iproute2 (`ip`), and sysctls such as
// IPv4 forwarding. Like every subprocess use in WG-Guard it goes through
// subprocess.Runner with explicit argv (ADR-0001) — a netlink library would be
// a second, unaudited path to the same kernel state.
package network

import (
	"context"
	"fmt"
	"strings"

	"github.com/Sir-Adnan/wg-guard/internal/subprocess"
)

// Links manages tunnel interface links through `ip`.
type Links struct {
	Run subprocess.Runner
}

func ipArgv(args ...string) []string { return append([]string{"ip"}, args...) }

func (l *Links) run(ctx context.Context, argv []string) (subprocess.Result, error) {
	res, err := l.Run.Run(ctx, argv)
	if err != nil {
		return res, fmt.Errorf("network: %s: %w", strings.Join(argv, " "), err)
	}
	return res, nil
}

// CreateAWG creates an amneziawg-typed link. mtu > 0 is applied in the same
// bring-up sequence.
func (l *Links) CreateAWG(ctx context.Context, name string, mtu int) error {
	if _, err := l.run(ctx, ipArgv("link", "add", name, "type", "amneziawg")); err != nil {
		return err
	}
	if mtu > 0 {
		if _, err := l.run(ctx, ipArgv("link", "set", "dev", name, "mtu", fmt.Sprint(mtu))); err != nil {
			return err
		}
	}
	return nil
}

// AddAddress assigns one CIDR address (the interface gateway, e.g.
// "10.8.0.1/24") to the link.
func (l *Links) AddAddress(ctx context.Context, name, cidr string) error {
	_, err := l.run(ctx, ipArgv("addr", "add", cidr, "dev", name))
	return err
}

// SetUp brings the link up.
func (l *Links) SetUp(ctx context.Context, name string) error {
	_, err := l.run(ctx, ipArgv("link", "set", "dev", name, "up"))
	return err
}

// Delete removes the link. A missing link is reported as
// ErrLinkNotFound-shaped: (false, nil) semantics are avoided in favor of an
// error callers can classify via LinkMissing.
func (l *Links) Delete(ctx context.Context, name string) error {
	res, err := l.Run.Run(ctx, ipArgv("link", "del", "dev", name))
	if err != nil {
		if LinkMissing(err, string(res.Stderr)) {
			return fmt.Errorf("network: ip link del %s: %w", name, ErrLinkNotFound)
		}
		return fmt.Errorf("network: ip link del %s: %w", name, err)
	}
	return nil
}

// ErrLinkNotFound reports that the requested link does not exist.
var ErrLinkNotFound = fmt.Errorf("network: link not found")

// LinkMissing classifies `ip link show <name>` failures: iproute2 exits 1
// with "Device \"name\" does not exist." for absent links.
func LinkMissing(err error, stderr string) bool {
	if err == nil {
		return false
	}
	return strings.Contains(stderr, "does not exist") ||
		strings.Contains(stderr, "Cannot find device")
}

// Exists reports whether the named link is present.
func (l *Links) Exists(ctx context.Context, name string) (bool, error) {
	res, err := l.Run.Run(ctx, ipArgv("link", "show", "dev", name))
	if err != nil {
		if LinkMissing(err, string(res.Stderr)) {
			return false, nil
		}
		return false, fmt.Errorf("network: ip link show %s: %w", name, err)
	}
	return true, nil
}
