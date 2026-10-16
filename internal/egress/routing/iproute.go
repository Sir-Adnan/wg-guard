package routing

import (
	"context"
	"encoding/json"
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
	return []string{"ip", "-4", "rule", op, "priority", strconv.Itoa(RulePriority),
		"fwmark", fmt.Sprintf("0x%x/0x%x", mark, MarkMask), "table", strconv.Itoa(table)}
}

// syncPolicy converges fwmark rules and per-table default routes. Routes are
// replaced (idempotent); rules are added before stale ones are removed.
func syncPolicy(ctx context.Context, run subprocess.Runner, routes []RouteSpec) error {
	for _, r := range routes {
		if _, err := run.Run(ctx, []string{"ip", "-4", "route", "replace", "default", "dev", r.Link, "table", strconv.Itoa(r.Table)}); err != nil {
			return fmt.Errorf("routing: route table %d: %w", r.Table, err)
		}
	}
	res, err := run.Run(ctx, []string{"ip", "-j", "-4", "rule", "show"})
	if err != nil {
		return fmt.Errorf("routing: list policy rules: %w", err)
	}
	live, err := ownedRules(res.Stdout)
	if err != nil {
		return err
	}
	want := map[uint32]int{}
	for _, r := range routes {
		want[r.Mark] = r.Table
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
		if _, err := run.Run(ctx, ruleArgs("add", m, want[m])); err != nil {
			return fmt.Errorf("routing: add policy rule: %w", err)
		}
	}
	for m, table := range live {
		if want[m] == table {
			continue
		}
		if _, err := run.Run(ctx, ruleArgs("del", m, table)); err != nil {
			return fmt.Errorf("routing: remove stale policy rule: %w", err)
		}
	}
	return nil
}
