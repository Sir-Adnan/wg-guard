package routing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/Sir-Adnan/wg-guard/internal/subprocess"
)

type liveRule struct {
	Priority int    `json:"priority"`
	Fwmark   string `json:"fwmark"`
	Fwmask   string `json:"fwmask"`
	Table    string `json:"table"`
}

func parseHex(s string) (uint32, bool) {
	v, err := strconv.ParseUint(strings.TrimPrefix(strings.ToLower(s), "0x"), 16, 32)
	return uint32(v), err == nil
}

// ownedRules returns the live fwmark rules egress routing owns: our priority
// and our exact mark mask. Foreign rules are never touched.
func ownedRules(data []byte) (map[uint32]int, error) {
	var rules []liveRule
	if len(data) > 1<<20 || json.Unmarshal(data, &rules) != nil {
		return nil, fmt.Errorf("routing: unreadable policy rule inventory")
	}
	out := map[uint32]int{}
	for _, r := range rules {
		if r.Priority != RulePriority || r.Fwmark == "" {
			continue
		}
		mask, ok := parseHex(r.Fwmask)
		if !ok || mask != MarkMask {
			continue
		}
		mark, ok := parseHex(r.Fwmark)
		if !ok {
			continue
		}
		table, err := strconv.Atoi(r.Table)
		if err != nil {
			continue
		}
		out[mark] = table
	}
	return out, nil
}

func ruleArgs(op string, mark uint32, table int) []string {
	return familyRuleArgs("-4", op, mark, table)
}

func familyRuleArgs(family, op string, mark uint32, table int) []string {
	return []string{"ip", family, "rule", op, "priority", strconv.Itoa(RulePriority),
		"fwmark", fmt.Sprintf("0x%x/0x%x", mark, MarkMask), "table", strconv.Itoa(table)}
}

// fallthroughMetric is the lowest priority, so the link route always wins
// while the link exists.
const fallthroughMetric = 4294967295

// syncPolicy converges IPv4 fwmark rules and per-table default routes.
// Routes are replaced (idempotent); rules are added before stale ones are
// removed. A route that fails does not stop the others, and its rule is left
// out so the mark falls through to the main table instead of an empty one.
func syncPolicy(ctx context.Context, run subprocess.Runner, routes []RouteSpec) error {
	_, err := syncFamily(ctx, run, "-4", routes)
	return err
}

// syncPolicy6 converges the IPv6 rules and routes of bridged links. It
// returns the links whose IPv6 route could not be installed (IPv6 disabled
// on the host, for example): their bridge falls back to IPv4.
func syncPolicy6(ctx context.Context, run subprocess.Runner, routes []RouteSpec) (map[string]bool, error) {
	return syncFamily(ctx, run, "-6", routes)
}

func syncFamily(ctx context.Context, run subprocess.Runner, family string, routes []RouteSpec) (map[string]bool, error) {
	var errs []error
	failed := map[string]bool{}
	want := map[uint32]int{}
	for _, r := range routes {
		table := strconv.Itoa(r.Table)
		// The kernel deletes the link route when the link goes down or away;
		// the unreachable route behind it stops marked traffic from falling
		// through to the main table and leaving directly from this node.
		if _, err := run.Run(ctx, []string{"ip", family, "route", "replace", "unreachable", "default", "metric", strconv.Itoa(fallthroughMetric), "table", table}); err != nil {
			errs = append(errs, fmt.Errorf("routing: route table %d guard: %w", r.Table, err))
			failed[r.Link] = true
			continue
		}
		if _, err := run.Run(ctx, []string{"ip", family, "route", "replace", "default", "dev", r.Link, "table", table}); err != nil {
			errs = append(errs, fmt.Errorf("routing: route table %d: %w", r.Table, err))
			failed[r.Link] = true
			continue
		}
		want[r.Mark] = r.Table
	}
	res, err := run.Run(ctx, []string{"ip", "-j", family, "rule", "show"})
	if err != nil {
		return failed, errors.Join(append(errs, fmt.Errorf("routing: list policy rules: %w", err))...)
	}
	live := map[uint32]int{}
	if strings.TrimSpace(string(res.Stdout)) != "" {
		if live, err = ownedRules(res.Stdout); err != nil {
			return failed, errors.Join(append(errs, err)...)
		}
	}
	marks := make([]uint32, 0, len(want))
	for m := range want {
		marks = append(marks, m)
	}
	sort.Slice(marks, func(i, j int) bool { return marks[i] < marks[j] })
	for _, m := range marks {
		if table, ok := live[m]; ok && table == want[m] {
			continue
		}
		if _, err := run.Run(ctx, familyRuleArgs(family, "add", m, want[m])); err != nil {
			errs = append(errs, fmt.Errorf("routing: add policy rule: %w", err))
			for _, r := range routes {
				if r.Mark == m {
					failed[r.Link] = true
				}
			}
		}
	}
	for m, table := range live {
		if want[m] == table {
			continue
		}
		if _, err := run.Run(ctx, familyRuleArgs(family, "del", m, table)); err != nil {
			errs = append(errs, fmt.Errorf("routing: remove stale policy rule: %w", err))
		}
	}
	return failed, errors.Join(errs...)
}
