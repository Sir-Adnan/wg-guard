// Package doctor implements `wg-guard doctor` (docs/operations/runbook.md):
// read-only environment and state checks with actionable remedies, plus
// `--fix` which re-runs the boot repairs (interface reconciliation, nftables,
// tc, sysctls) through the same orchestration `serve` uses. The fix path
// refuses to run while the service is up — it would race the serialized
// reconciler for the AWG subprocess.
package doctor

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log/slog"
	"net"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/backup"
	"github.com/Sir-Adnan/wg-guard/internal/boot"
	"github.com/Sir-Adnan/wg-guard/internal/config"
	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
	"github.com/Sir-Adnan/wg-guard/internal/settings"
	"github.com/Sir-Adnan/wg-guard/internal/shaper"
	"github.com/Sir-Adnan/wg-guard/internal/subprocess"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel"
)

// Status is one check outcome.
type Status string

const (
	StatusPass Status = "pass"
	StatusWarn Status = "warn"
	StatusFail Status = "fail"
	StatusSkip Status = "skip"
)

// Check is one diagnostic line. Detail and Remedy never contain secrets.
type Check struct {
	Name   string
	Status Status
	Detail string
	Remedy string
}

// Report is the full doctor output.
type Report struct {
	Checks []Check
	Fixes  []string // what --fix changed, in order
}

// Failures counts checks that need attention (exit-code driver).
func (r *Report) Failures() int {
	n := 0
	for _, c := range r.Checks {
		if c.Status == StatusFail {
			n++
		}
	}
	return n
}

// Warnings counts warn-grade checks.
func (r *Report) Warnings() int {
	n := 0
	for _, c := range r.Checks {
		if c.Status == StatusWarn {
			n++
		}
	}
	return n
}

// Deps wires the doctor. DB/Reg may be nil where a check cannot run.
type Deps struct {
	Cfg        *config.Config
	ConfigPath string
	DB         *database.DB
	Reg        *settings.Registry
	Ring       *secrets.KeyRing
	Backend    tunnel.Backend
	Run        subprocess.Runner
	Shaper     *shaper.Manager
	Log        *slog.Logger

	// Fix applies the safe boot repairs after the read-only pass.
	Fix bool
	// ServiceUp lets the caller pre-check whether the node is serving
	// (the fix path refuses when it is).
	ServiceUp bool
}

type doctor struct {
	d      Deps
	report Report
}

func (d *doctor) add(name string, status Status, detail, remedy string) {
	d.report.Checks = append(d.report.Checks, Check{
		Name: name, Status: status, Detail: detail, Remedy: remedy,
	})
}

// Run executes every check in order, then (--fix) the boot repairs and a
// re-check of the affected areas.
func Run(ctx context.Context, d Deps) (*Report, error) {
	doc := &doctor{d: d}
	doc.checkPlatform()
	doc.checkPrivileges()
	doc.checkDataDir()
	doc.checkMasterKey()
	doc.checkTools(ctx)
	doc.checkKernelModule()
	doc.checkDatabase(ctx)
	doc.checkInterfaces(ctx)
	doc.checkFirewall(ctx)
	doc.checkSysctls()
	doc.checkShaper(ctx)
	doc.checkDisk()
	doc.checkEndpoint(ctx)
	doc.checkTLSCert()
	doc.checkClock()
	doc.checkBackups()

	if d.Fix {
		if d.ServiceUp {
			return &doc.report, fmt.Errorf(
				"doctor --fix refuses to run while the service is up (it would race the reconciler); stop the service first")
		}
		applied, err := applyFixes(ctx, d)
		if err != nil {
			return &doc.report, fmt.Errorf("doctor: fix: %w", err)
		}
		doc.report.Fixes = applied
		// Re-check the repaired areas.
		doc.checkDataDir()
		doc.checkDatabase(ctx)
		doc.checkInterfaces(ctx)
		doc.checkFirewall(ctx)
		doc.checkSysctls()
		doc.checkShaper(ctx)
	}
	return &doc.report, nil
}

// applyFixes runs the boot bring-up (same orchestration as serve): tooling,
// forwarding, reconcile, nftables, tc, ufw coexistence. Indirect for tests.
var bringUp = func(ctx context.Context, d Deps) (fixResult, error) {
	return boot.BringUp(ctx, boot.Deps{
		DB: d.DB, Ring: d.Ring, Settings: d.Reg, Backend: d.Backend,
		Run: d.Run, Shaper: d.Shaper,
	})
}

// fixResult is the slice of boot.Result the fix summary prints.
type fixResult = *boot.Result

func applyFixes(ctx context.Context, d Deps) ([]string, error) {
	if d.DB == nil || d.Reg == nil || d.Backend == nil {
		return nil, fmt.Errorf("fix requires the database, settings and backend")
	}
	res, err := bringUp(ctx, d)
	if err != nil {
		return nil, err
	}
	var fixes []string
	fixes = append(fixes, fmt.Sprintf("reconciled tunnels (created %d, updated %d, removed %d; peers +%d/-%d)",
		res.Reconcile.InterfacesCreated, res.Reconcile.InterfacesUpdated, res.Reconcile.InterfacesRemoved,
		res.Reconcile.PeersAdded, res.Reconcile.PeersRemoved))
	fixes = append(fixes, fmt.Sprintf("nftables table applied for %d interface(s)", res.ManagedIfaces))
	if res.ShapedGroups > 0 {
		fixes = append(fixes, fmt.Sprintf("speed-limit groups ensured (%d)", res.ShapedGroups))
	}
	if res.ForwardingChanged {
		fixes = append(fixes, "ipv4 forwarding enabled")
	}
	for _, f := range res.Findings {
		fixes = append(fixes, fmt.Sprintf("finding [%s]: %s — remedy: %s", f.Tool, f.Detail, f.Remedy))
	}
	return fixes, nil
}

// --- checks -------------------------------------------------------------------

func (d *doctor) checkPlatform() {
	d.add("platform", StatusPass, runtime.GOOS+"/"+runtime.GOARCH, "")
}

func (d *doctor) checkPrivileges() {
	st, detail := platformPrivileges()
	d.add("privileges", st, detail, "tunnel and firewall management requires root — run the service via systemd as designed")
}

func (d *doctor) checkDataDir() {
	dir := d.d.Cfg.DataDir
	st, err := os.Stat(dir)
	if err != nil {
		d.add("data-dir", StatusFail, fmt.Sprintf("%s: %v", dir, err),
			"create the data directory or fix the configured data_dir")
		return
	}
	if !st.IsDir() {
		d.add("data-dir", StatusFail, fmt.Sprintf("%s is not a directory", dir), "")
		return
	}
	if s := checkPerm(dir, 0o070); s != "" {
		d.add("data-dir", StatusWarn, s, "restrict the data directory (it holds the database and master key)")
		return
	}
	d.add("data-dir", StatusPass, dir, "")
}

func (d *doctor) checkMasterKey() {
	p := d.d.Cfg.MasterKeyFile
	st, err := os.Stat(p)
	if err != nil {
		d.add("master-key", StatusFail, fmt.Sprintf("%s: %v", p, err),
			"the master key is required to decrypt device secrets; restore it from backup")
		return
	}
	if s := checkPermFile(p, st); s != "" {
		d.add("master-key", StatusWarn, s, "chmod 600 the master key")
		return
	}
	d.add("master-key", StatusPass, p, "")
}

func (d *doctor) checkTools(ctx context.Context) {
	if d.d.Backend == nil {
		d.add("awg-tools", StatusSkip, "no backend wired", "")
		return
	}
	prober, ok := d.d.Backend.(interface {
		ToolsVersion(context.Context) (string, error)
	})
	if !ok {
		d.add("awg-tools", StatusSkip, "backend has no version probe (dev/fake)", "")
		return
	}
	v, err := prober.ToolsVersion(ctx)
	if err != nil {
		d.add("awg-tools", StatusFail, fmt.Sprintf("probe failed: %v", err),
			"install AmneziaWG tools from the pinned PPA (docs/integrations/amneziawg.md)")
		return
	}
	d.add("awg-tools", StatusPass, v, "")
}

func (d *doctor) checkKernelModule() {
	loaded, detail := kernelModuleLoaded("amneziawg")
	if loaded {
		d.add("kernel-module", StatusPass, detail, "")
		return
	}
	d.add("kernel-module", StatusWarn, detail,
		"tunnels will use the userspace daemon; for the kernel module install amneziawg-dkms (docs/integrations/amneziawg.md)")
}

func (d *doctor) checkDatabase(ctx context.Context) {
	if d.d.DB == nil {
		d.add("database", StatusSkip, "no database wired", "")
		return
	}
	var integrity string
	if err := d.d.DB.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil {
		d.add("database", StatusFail, fmt.Sprintf("integrity_check: %v", err),
			"stop the service and restore the latest backup (runbook: backup/restore)")
		return
	}
	if integrity != "ok" {
		d.add("database", StatusFail, "integrity_check: "+integrity,
			"stop the service and restore the latest backup (runbook: backup/restore)")
		return
	}
	d.add("database", StatusPass, "integrity ok", "")
}

func (d *doctor) checkInterfaces(ctx context.Context) {
	if d.d.DB == nil || d.d.Backend == nil {
		d.add("interfaces", StatusSkip, "database or backend not wired", "")
		return
	}
	rows, err := d.d.DB.QueryContext(ctx,
		`SELECT name, listen_port FROM tunnel_interfaces WHERE enabled = 1 ORDER BY name`)
	if err != nil {
		d.add("interfaces", StatusSkip, "query failed: "+err.Error(), "")
		return
	}
	defer rows.Close()
	type row struct {
		name string
		port int
	}
	var want []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.name, &r.port); err == nil {
			want = append(want, r)
		}
	}
	if len(want) == 0 {
		d.add("interfaces", StatusPass, "no enabled interfaces", "")
		return
	}
	var missing, drift, peerMismatch []string
	for _, w := range want {
		state, err := d.d.Backend.Dump(ctx, w.name)
		if err != nil {
			missing = append(missing, w.name)
			continue
		}
		if state.ListenPort != w.port {
			drift = append(drift, fmt.Sprintf("%s (port %d != %d)", w.name, state.ListenPort, w.port))
		}
		if n := countEnabledDevices(ctx, d.d.DB, w.name); n >= 0 && n != len(state.Peers) {
			peerMismatch = append(peerMismatch,
				fmt.Sprintf("%s (%d peers in kernel, %d enabled devices)", w.name, len(state.Peers), n))
		}
	}
	if len(missing) > 0 {
		d.add("interfaces", StatusFail,
			"missing from backend: "+strings.Join(missing, ", "),
			"wg-guard doctor --fix (recreates the interface from the database)")
		return
	}
	if len(drift)+len(peerMismatch) > 0 {
		d.add("interfaces", StatusWarn,
			strings.Join(append(drift, peerMismatch...), "; "),
			"wg-guard doctor --fix (re-applies configs and peer sets)")
		return
	}
	d.add("interfaces", StatusPass,
		fmt.Sprintf("%d enabled interface(s) match the backend", len(want)), "")
}

// countEnabledDevices counts devices that should hold peers on the named
// interface (enabled device of an enabled account). It is a heuristic for
// the doctor warn — the reconciler stays the authority.
func countEnabledDevices(ctx context.Context, db *database.DB, ifaceName string) int {
	var n int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM devices
		WHERE enabled = 1 AND interface_id = (SELECT id FROM tunnel_interfaces WHERE name = ?)
		AND user_id IN (SELECT id FROM users WHERE enabled = 1)`, ifaceName).Scan(&n)
	if err != nil {
		return -1
	}
	return n
}

func (d *doctor) checkFirewall(ctx context.Context) {
	if d.d.Run == nil {
		d.add("nftables", StatusSkip, "no runner wired", "")
		return
	}
	if _, err := d.d.Run.Run(ctx, []string{"nft", "list", "table", "inet", "wgguard"}); err != nil {
		d.add("nftables", StatusWarn, "table inet wgguard is missing",
			"wg-guard doctor --fix (re-applies the namespaced table)")
		return
	}
	d.add("nftables", StatusPass, "table inet wgguard present", "")
}

func (d *doctor) checkSysctls() {
	v, ok := readIPForward()
	if !ok {
		d.add("sysctl", StatusSkip, "ip_forward not readable on this platform", "")
		return
	}
	if v != "1" {
		d.add("sysctl", StatusWarn, "net.ipv4.ip_forward is 0",
			"wg-guard doctor --fix (or sysctl -w net.ipv4.ip_forward=1)")
		return
	}
	d.add("sysctl", StatusPass, "net.ipv4.ip_forward=1", "")
}

func (d *doctor) checkShaper(ctx context.Context) {
	if d.d.DB == nil || d.d.Shaper == nil {
		d.add("shaper", StatusSkip, "database or shaper not wired", "")
		return
	}
	groups, err := shaper.LoadGroups(ctx, d.d.DB)
	if err != nil {
		d.add("shaper", StatusSkip, "load: "+err.Error(), "")
		return
	}
	if len(groups) == 0 {
		d.add("shaper", StatusPass, "no speed limits configured", "")
		return
	}
	byIface := map[string]bool{}
	for _, g := range groups {
		byIface[g.InterfaceName] = true
	}
	var names []string
	for n := range byIface {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if _, err := d.d.Run.Run(ctx, []string{"tc", "qdisc", "show", "dev", n}); err != nil {
			d.add("shaper", StatusWarn,
				fmt.Sprintf("speed limits configured for %s but tc state is unreadable", n),
				"wg-guard doctor --fix (rebuilds the qdisc tree)")
			return
		}
	}
	d.add("shaper", StatusPass,
		fmt.Sprintf("%d speed-limited group(s), tc state readable", len(groups)), "")
}

func (d *doctor) checkDisk() {
	free, total, ok := diskUsage(d.d.Cfg.DataDir)
	if !ok {
		d.add("disk", StatusSkip, "free space unavailable on this platform", "")
		return
	}
	pct := 0.0
	if total > 0 {
		pct = float64(free) / float64(total) * 100
	}
	GB := uint64(1) << 30
	detail := fmt.Sprintf("%.1f GiB free of %.1f GiB (%.0f%%)", float64(free)/float64(GB), float64(total)/float64(GB), pct)
	switch {
	case pct < 5:
		d.add("disk", StatusFail, detail, "free disk space — accounting samples and backups need headroom")
	case pct < 15:
		d.add("disk", StatusWarn, detail, "plan space for backups and traffic samples")
	default:
		d.add("disk", StatusPass, detail, "")
	}
}

func (d *doctor) checkEndpoint(ctx context.Context) {
	if d.d.Reg == nil {
		d.add("endpoint", StatusSkip, "settings not wired", "")
		return
	}
	ep, err := d.d.Reg.GetString(ctx, "node.endpoint")
	if err != nil || ep == "" {
		d.add("endpoint", StatusWarn, "node.endpoint is not set",
			"set it in Settings — client configs embed this host")
		return
	}
	host, _, err := net.SplitHostPort(ep)
	if err != nil {
		host = ep
	}
	if ip := net.ParseIP(host); ip != nil {
		d.add("endpoint", StatusPass, ep+" (literal IP)", "")
		return
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupHost(lookupCtx, host)
	if err != nil || len(addrs) == 0 {
		d.add("endpoint", StatusWarn, fmt.Sprintf("%s does not resolve", host),
			"fix DNS for node.endpoint — new client configs will be broken")
		return
	}
	d.add("endpoint", StatusPass, fmt.Sprintf("%s → %s", ep, addrs[0]), "")
}

// loadCertificateNotAfter parses the leaf certificate expiry.
func loadCertificateNotAfter(path string) (time.Time, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return time.Time{}, fmt.Errorf("no PEM certificate block")
	}
	crt, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}, err
	}
	return crt.NotAfter, nil
}

func (d *doctor) checkTLSCert() {
	if d.d.Cfg.TLS.Mode != config.TLSModeManual {
		d.add("tls-cert", StatusSkip, "tls.mode="+string(d.d.Cfg.TLS.Mode), "")
		return
	}
	st, err := os.Stat(d.d.Cfg.TLS.CertFile)
	if err != nil {
		d.add("tls-cert", StatusFail, fmt.Sprintf("cert file: %v", err),
			"install the configured tls.cert_file")
		return
	}
	_ = st
	pe, err := loadCertificateNotAfter(d.d.Cfg.TLS.CertFile)
	if err != nil {
		d.add("tls-cert", StatusFail, "parse: "+err.Error(), "replace the certificate")
		return
	}
	days := time.Until(pe).Hours() / 24
	switch {
	case days <= 0:
		d.add("tls-cert", StatusFail, fmt.Sprintf("expired %s", pe.Format("2006-01-02")),
			"renew the certificate and restart")
	case days < 30:
		d.add("tls-cert", StatusWarn, fmt.Sprintf("expires in %.0f days", days), "renew the certificate")
	default:
		d.add("tls-cert", StatusPass, fmt.Sprintf("valid until %s", pe.Format("2006-01-02")), "")
	}
}

// checkClock asks systemd-timesyncd whether NTP is synchronized
// (best-effort; anything without timedatectl skips honestly). Expiry
// enforcement runs in UTC, so a skewed clock is a correctness risk.
func (d *doctor) checkClock() {
	if d.d.Run == nil {
		d.add("clock", StatusSkip, "no runner wired", "")
		return
	}
	res, err := d.d.Run.Run(context.Background(),
		[]string{"timedatectl", "show", "-p", "NTPSynchronized", "--value"})
	if err != nil {
		d.add("clock", StatusSkip, "timedatectl unavailable", "")
		return
	}
	if strings.TrimSpace(string(res.Stdout)) != "yes" {
		d.add("clock", StatusWarn, "NTP not synchronized",
			"expiry sweeps compare against wall-clock UTC — enable time synchronization")
		return
	}
	d.add("clock", StatusPass, "NTP synchronized", "")
}

func (d *doctor) checkBackups() {
	backupSvc := &backup.Service{Cfg: d.d.Cfg}
	arcs, err := backupSvc.List()
	if err != nil {
		d.add("backups", StatusSkip, "list: "+err.Error(), "")
		return
	}
	if d.d.Reg != nil && d.d.DB != nil {
		var schedules int
		_ = d.d.DB.QueryRowContext(context.Background(),
			`SELECT COUNT(*) FROM backup_schedules WHERE enabled = 1`).Scan(&schedules)
		if schedules == 0 && len(arcs) == 0 {
			d.add("backups", StatusWarn, "no archives and no enabled schedules",
				"create a schedule in Settings → Backups — a node without backups is one disk away from data loss")
			return
		}
	}
	if len(arcs) == 0 {
		d.add("backups", StatusSkip, "no archives yet", "")
		return
	}
	age := time.Since(arcs[0].ModTime).Round(time.Hour)
	enc := ""
	if arcs[0].Encrypted {
		enc = ", encrypted"
	}
	st := StatusPass
	var remedy string
	if age > 7*24*time.Hour {
		st = StatusWarn
		remedy = "check the backup schedules — the newest archive is stale"
	}
	d.add("backups", st, fmt.Sprintf("newest: %s (%d KiB%s, %s ago)",
		arcs[0].Name, arcs[0].Size>>10, enc, age), remedy)
}
