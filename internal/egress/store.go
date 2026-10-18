package egress

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel"
)

// Bounds that keep rendering, health probing and the panel small.
const (
	MaxBalancers       = 32
	MaxBalancerMembers = 16
	MaxRules           = 128
)

// WARPRegistration is what a WARP registrar returns for a fresh device.
type WARPRegistration struct {
	DeviceID      string
	Token         string
	AccountType   string
	Plus          bool
	PeerPublicKey string
	Endpoint      string // host:port
	AddressV4     string // bare IPv4
}

// WARPRegistrar talks to the Cloudflare client API (internal/egress/warp).
type WARPRegistrar interface {
	Register(ctx context.Context, publicKey string) (WARPRegistration, error)
	// ApplyLicense binds a WARP+ license and returns the resulting account.
	ApplyLicense(ctx context.Context, deviceID, token, license string) (accountType string, plus bool, err error)
}

// ErrWARPUnavailable reports a failed registration call. The panel shows a
// fixed message; Cloudflare's response body is never displayed.
var ErrWARPUnavailable = errors.New("egress: WARP registration failed")

// Service stores egress routing state.
type Service struct {
	db   *database.DB
	ring *secrets.KeyRing
	now  func() time.Time
	// WARP is nil when WARP registration is not wired (tests, dev).
	WARP WARPRegistrar
}

// NewService returns the egress store.
func NewService(db *database.DB, ring *secrets.KeyRing) *Service {
	return &Service{db: db, ring: ring, now: time.Now}
}

type storedConfig struct {
	V        int           `json:"v"`
	Tunnel   *TunnelConfig `json:"tunnel,omitempty"`
	SourceIP string        `json:"source_ip,omitempty"`
	WARP     *WARPAccount  `json:"warp,omitempty"`
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseTS(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// --- outbounds -----------------------------------------------------------------

const outboundColumns = `id, name, kind, slot, enabled, config_json, created_at, updated_at`

func scanOutbound(scan func(...any) error) (*Outbound, error) {
	var (
		o                Outbound
		kind, cfg        string
		enabled          int
		created, updated string
	)
	if err := scan(&o.ID, &o.Name, &kind, &o.Slot, &enabled, &cfg, &created, &updated); err != nil {
		return nil, err
	}
	o.Kind, o.Enabled = Kind(kind), enabled == 1
	o.CreatedAt, o.UpdatedAt = parseTS(created), parseTS(updated)
	var sc storedConfig
	if err := json.Unmarshal([]byte(cfg), &sc); err != nil {
		return nil, fmt.Errorf("egress: stored outbound %s config: %w", o.ID, err)
	}
	o.Tunnel, o.SourceIP, o.WARP = sc.Tunnel, sc.SourceIP, sc.WARP
	return &o, nil
}

// ListOutbounds returns every outbound ordered by name.
func (s *Service) ListOutbounds(ctx context.Context) ([]*Outbound, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+outboundColumns+` FROM egress_outbounds ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, fmt.Errorf("egress: list outbounds: %w", err)
	}
	defer rows.Close()
	var out []*Outbound
	for rows.Next() {
		o, err := scanOutbound(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// GetOutbound returns one outbound.
func (s *Service) GetOutbound(ctx context.Context, id string) (*Outbound, error) {
	o, err := scanOutbound(s.db.QueryRowContext(ctx, `SELECT `+outboundColumns+` FROM egress_outbounds WHERE id = ?`, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("egress: get outbound: %w", err)
	}
	return o, nil
}

// OutboundSecret decrypts one outbound's sealed material.
func (s *Service) OutboundSecret(ctx context.Context, id string) (Secret, error) {
	var sealed []byte
	err := s.db.QueryRowContext(ctx, `SELECT secret_encrypted FROM egress_outbounds WHERE id = ?`, id).Scan(&sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return Secret{}, ErrNotFound
	}
	if err != nil {
		return Secret{}, fmt.Errorf("egress: load secret: %w", err)
	}
	return s.openSecret(sealed)
}

func (s *Service) openSecret(sealed []byte) (Secret, error) {
	var sec Secret
	if len(sealed) == 0 {
		return sec, nil
	}
	plain, err := s.ring.Decrypt(sealed)
	if err != nil {
		return sec, fmt.Errorf("egress: decrypt outbound secret: %w", err)
	}
	defer clear(plain)
	if err := json.Unmarshal(plain, &sec); err != nil {
		return Secret{}, fmt.Errorf("egress: outbound secret is malformed")
	}
	return sec, nil
}

func (s *Service) sealSecret(sec Secret) ([]byte, error) {
	if sec == (Secret{}) {
		return nil, nil
	}
	plain, err := json.Marshal(sec)
	if err != nil {
		return nil, err
	}
	defer clear(plain)
	return s.ring.Encrypt(plain)
}

// OutboundInput is the editable part of an outbound. Kind-specific creation
// data arrives through the dedicated constructors.
type OutboundInput struct {
	Name     string
	Enabled  *bool
	Endpoint string // tunneled kinds: replacement endpoint; "" keeps
	MTU      int    // tunneled kinds: 0 keeps
	SourceIP string // source_ip: replacement; "" keeps
}

// CreateWireGuard imports a WireGuard/AmneziaWG client config as an outbound.
func (s *Service) CreateWireGuard(ctx context.Context, name, config string) (*Outbound, error) {
	if err := validName(name); err != nil {
		return nil, err
	}
	parsed, err := ParseClientConfig(config)
	if err != nil {
		return nil, err
	}
	return s.insertOutbound(ctx, &Outbound{Name: strings.TrimSpace(name), Kind: KindWireGuard, Enabled: true, Tunnel: &parsed.Tunnel}, parsed.Secret)
}

// CreateSourceIP adds an outbound that leaves through another public IPv4
// address owned by this server.
func (s *Service) CreateSourceIP(ctx context.Context, name, address string) (*Outbound, error) {
	if err := validName(name); err != nil {
		return nil, err
	}
	ip, err := validSourceIP(address)
	if err != nil {
		return nil, err
	}
	return s.insertOutbound(ctx, &Outbound{Name: strings.TrimSpace(name), Kind: KindSourceIP, Enabled: true, SourceIP: ip}, Secret{})
}

func validSourceIP(address string) (string, error) {
	a, err := netip.ParseAddr(strings.TrimSpace(address))
	if err != nil || !a.Is4() || a.IsLoopback() || a.IsUnspecified() || a.IsMulticast() || a.IsLinkLocalUnicast() {
		return "", invalid("source_ip", "source_ip_invalid")
	}
	return a.String(), nil
}

// CreateWARP registers a new Cloudflare WARP device and stores it. A non-empty
// license upgrades it to WARP+. endpoint overrides the advertised endpoint.
func (s *Service) CreateWARP(ctx context.Context, name, license, endpoint string) (*Outbound, error) {
	if err := validName(name); err != nil {
		return nil, err
	}
	license = strings.TrimSpace(license)
	if err := validLicense(license); err != nil {
		return nil, err
	}
	endpoint = strings.TrimSpace(endpoint)
	if endpoint != "" {
		if err := validEndpoint(endpoint); err != nil {
			return nil, err
		}
	}
	if s.WARP == nil {
		return nil, ErrWARPUnavailable
	}
	keys, err := tunnel.GenerateKeyPair()
	if err != nil {
		return nil, err
	}
	reg, err := s.WARP.Register(ctx, keys.Public)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrWARPUnavailable, err)
	}
	addr, err := netip.ParseAddr(reg.AddressV4)
	if err != nil || !addr.Is4() || tunnel.ValidatePublicKey(reg.PeerPublicKey) != nil || validEndpoint(reg.Endpoint) != nil {
		return nil, fmt.Errorf("%w: unexpected registration response", ErrWARPUnavailable)
	}
	account := &WARPAccount{DeviceID: reg.DeviceID, AccountType: reg.AccountType, Plus: reg.Plus, RegisteredAt: s.now().UTC()}
	secret := Secret{PrivateKey: keys.Private, WARPToken: reg.Token}
	if license != "" {
		accountType, plus, err := s.WARP.ApplyLicense(ctx, reg.DeviceID, reg.Token, license)
		if err != nil {
			return nil, invalid("license", "license_rejected")
		}
		account.AccountType, account.Plus = accountType, plus
		secret.WARPLicense = license
	}
	if endpoint == "" {
		endpoint = reg.Endpoint
	}
	cfg := &TunnelConfig{
		Address: netip.PrefixFrom(addr, 32).String(), MTU: DefaultTunnelMTU,
		PeerPublicKey: reg.PeerPublicKey, Endpoint: endpoint, Keepalive: 25,
	}
	return s.insertOutbound(ctx, &Outbound{Name: strings.TrimSpace(name), Kind: KindWARP, Enabled: true, Tunnel: cfg, WARP: account}, secret)
}

// UpdateWARPLicense binds (or replaces) the WARP+ license of a WARP outbound.
func (s *Service) UpdateWARPLicense(ctx context.Context, id, license string) (*Outbound, error) {
	license = strings.TrimSpace(license)
	if license == "" {
		return nil, invalid("license", "license_required")
	}
	if err := validLicense(license); err != nil {
		return nil, err
	}
	o, err := s.GetOutbound(ctx, id)
	if err != nil {
		return nil, err
	}
	if o.Kind != KindWARP || o.WARP == nil {
		return nil, invalid("license", "license_not_warp")
	}
	if s.WARP == nil {
		return nil, ErrWARPUnavailable
	}
	sec, err := s.OutboundSecret(ctx, id)
	if err != nil {
		return nil, err
	}
	accountType, plus, err := s.WARP.ApplyLicense(ctx, o.WARP.DeviceID, sec.WARPToken, license)
	if err != nil {
		return nil, invalid("license", "license_rejected")
	}
	o.WARP.AccountType, o.WARP.Plus = accountType, plus
	sec.WARPLicense = license
	if err := s.updateOutboundRow(ctx, o, &sec); err != nil {
		return nil, err
	}
	return s.GetOutbound(ctx, id)
}

// licenseRe-free check: WARP+ keys are three dash-separated 8-character
// groups of letters and digits ("xxxxxxxx-xxxxxxxx-xxxxxxxx").
// ValidateLicense checks the format of a WARP+ license key without any
// network call (the panel validates before its first write).
func ValidateLicense(license string) error { return validLicense(strings.TrimSpace(license)) }

func validLicense(license string) error {
	if license == "" {
		return nil
	}
	parts := strings.Split(license, "-")
	if len(parts) != 3 {
		return invalid("license", "license_invalid")
	}
	for _, p := range parts {
		if len(p) != 8 {
			return invalid("license", "license_invalid")
		}
		for _, r := range p {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
				return invalid("license", "license_invalid")
			}
		}
	}
	return nil
}

func (s *Service) insertOutbound(ctx context.Context, o *Outbound, sec Secret) (*Outbound, error) {
	sealed, err := s.sealSecret(sec)
	if err != nil {
		return nil, err
	}
	cfg, err := json.Marshal(storedConfig{V: 1, Tunnel: o.Tunnel, SourceIP: o.SourceIP, WARP: o.WARP})
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	o.ID, o.CreatedAt, o.UpdatedAt = domain.NewID(), now, now
	err = s.db.WithTx(ctx, func(tx *sql.Tx) error {
		used := map[int]bool{}
		rows, err := tx.QueryContext(ctx, `SELECT slot FROM egress_outbounds`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var n int
			if err := rows.Scan(&n); err != nil {
				rows.Close()
				return err
			}
			used[n] = true
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		o.Slot = 0
		for n := 1; n <= MaxSlots; n++ {
			if !used[n] {
				o.Slot = n
				break
			}
		}
		if o.Slot == 0 {
			return invalid("name", "outbounds_full")
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO egress_outbounds
			(id, name, kind, slot, enabled, config_json, secret_encrypted, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			o.ID, o.Name, string(o.Kind), o.Slot, boolInt(o.Enabled), string(cfg), sealed, ts(now), ts(now))
		if isUnique(err) {
			return invalid("name", "name_taken")
		}
		return err
	})
	if err != nil {
		if _, ok := AsValidation(err); ok {
			return nil, err
		}
		return nil, fmt.Errorf("egress: insert outbound: %w", err)
	}
	return o, nil
}

// UpdateOutbound edits name, enabled state, endpoint, MTU or source address.
func (s *Service) UpdateOutbound(ctx context.Context, id string, in OutboundInput) (*Outbound, error) {
	o, err := s.GetOutbound(ctx, id)
	if err != nil {
		return nil, err
	}
	if in.Name != "" {
		if err := validName(in.Name); err != nil {
			return nil, err
		}
		o.Name = strings.TrimSpace(in.Name)
	}
	if in.Enabled != nil {
		o.Enabled = *in.Enabled
	}
	if o.Kind.Tunneled() && o.Tunnel != nil {
		if e := strings.TrimSpace(in.Endpoint); e != "" {
			if err := validEndpoint(e); err != nil {
				return nil, err
			}
			o.Tunnel.Endpoint = e
		}
		if in.MTU != 0 {
			if in.MTU < 576 || in.MTU > 1500 {
				return nil, invalid("mtu", "mtu_invalid")
			}
			o.Tunnel.MTU = in.MTU
		}
	}
	if o.Kind == KindSourceIP && strings.TrimSpace(in.SourceIP) != "" {
		ip, err := validSourceIP(in.SourceIP)
		if err != nil {
			return nil, err
		}
		o.SourceIP = ip
	}
	if err := s.updateOutboundRow(ctx, o, nil); err != nil {
		return nil, err
	}
	return s.GetOutbound(ctx, id)
}

// ReplaceWireGuardConfig re-imports the client config of a wireguard outbound
// (rotated keys, a new server), keeping its identity and references.
func (s *Service) ReplaceWireGuardConfig(ctx context.Context, id, config string) (*Outbound, error) {
	o, err := s.GetOutbound(ctx, id)
	if err != nil {
		return nil, err
	}
	if o.Kind != KindWireGuard {
		return nil, invalid("config", "config_not_wireguard")
	}
	parsed, err := ParseClientConfig(config)
	if err != nil {
		return nil, err
	}
	o.Tunnel = &parsed.Tunnel
	if err := s.updateOutboundRow(ctx, o, &parsed.Secret); err != nil {
		return nil, err
	}
	return s.GetOutbound(ctx, id)
}

func (s *Service) updateOutboundRow(ctx context.Context, o *Outbound, sec *Secret) error {
	cfg, err := json.Marshal(storedConfig{V: 1, Tunnel: o.Tunnel, SourceIP: o.SourceIP, WARP: o.WARP})
	if err != nil {
		return err
	}
	now := ts(s.now())
	var res sql.Result
	if sec != nil {
		sealed, err := s.sealSecret(*sec)
		if err != nil {
			return err
		}
		res, err = s.db.ExecContext(ctx, `UPDATE egress_outbounds SET name = ?, enabled = ?, config_json = ?,
			secret_encrypted = ?, updated_at = ? WHERE id = ?`, o.Name, boolInt(o.Enabled), string(cfg), sealed, now, o.ID)
		if err != nil {
			if isUnique(err) {
				return invalid("name", "name_taken")
			}
			return fmt.Errorf("egress: update outbound: %w", err)
		}
	} else {
		res, err = s.db.ExecContext(ctx, `UPDATE egress_outbounds SET name = ?, enabled = ?, config_json = ?,
			updated_at = ? WHERE id = ?`, o.Name, boolInt(o.Enabled), string(cfg), now, o.ID)
		if err != nil {
			if isUnique(err) {
				return invalid("name", "name_taken")
			}
			return fmt.Errorf("egress: update outbound: %w", err)
		}
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteOutbound removes an outbound that no rule, balancer or the default
// route references. The error lists the referencing objects by name.
func (s *Service) DeleteOutbound(ctx context.Context, id string) error {
	refs, err := s.references(ctx, Target{Kind: TargetOutbound, ID: id})
	if err != nil {
		return err
	}
	if len(refs) > 0 {
		return invalidDetail("delete", "in_use", strings.Join(refs, ", "))
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM egress_outbounds WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("egress: delete outbound: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// references names the rules, balancers and default route that use target.
func (s *Service) references(ctx context.Context, target Target) ([]string, error) {
	var refs []string
	rules, err := s.ListRules(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range rules {
		if r.Target == target {
			refs = append(refs, r.Name)
		}
	}
	if target.Kind == TargetOutbound {
		balancers, err := s.ListBalancers(ctx)
		if err != nil {
			return nil, err
		}
		for _, b := range balancers {
			for _, m := range b.Members {
				if m.OutboundID == target.ID {
					refs = append(refs, b.Name)
					break
				}
			}
		}
	}
	st, err := s.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	if st.DefaultTarget == target {
		refs = append(refs, "default")
	}
	return refs, nil
}

// --- balancers -----------------------------------------------------------------

const balancerColumns = `id, name, strategy, fallback, members_json, enabled, created_at, updated_at`

func scanBalancer(scan func(...any) error) (*Balancer, error) {
	var (
		b                          Balancer
		strategy, fallback, member string
		enabled                    int
		created, updated           string
	)
	if err := scan(&b.ID, &b.Name, &strategy, &fallback, &member, &enabled, &created, &updated); err != nil {
		return nil, err
	}
	b.Strategy, b.Fallback, b.Enabled = Strategy(strategy), Fallback(fallback), enabled == 1
	b.CreatedAt, b.UpdatedAt = parseTS(created), parseTS(updated)
	if err := json.Unmarshal([]byte(member), &b.Members); err != nil {
		return nil, fmt.Errorf("egress: stored balancer %s members: %w", b.ID, err)
	}
	return &b, nil
}

// ListBalancers returns every balancer ordered by name.
func (s *Service) ListBalancers(ctx context.Context) ([]*Balancer, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+balancerColumns+` FROM egress_balancers ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, fmt.Errorf("egress: list balancers: %w", err)
	}
	defer rows.Close()
	var out []*Balancer
	for rows.Next() {
		b, err := scanBalancer(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// GetBalancer returns one balancer.
func (s *Service) GetBalancer(ctx context.Context, id string) (*Balancer, error) {
	b, err := scanBalancer(s.db.QueryRowContext(ctx, `SELECT `+balancerColumns+` FROM egress_balancers WHERE id = ?`, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("egress: get balancer: %w", err)
	}
	return b, nil
}

// BalancerInput creates or replaces a balancer.
type BalancerInput struct {
	Name     string
	Strategy Strategy
	Fallback Fallback
	Members  []Member
	Enabled  bool
}

func (s *Service) validateBalancer(ctx context.Context, in *BalancerInput) error {
	if err := validName(in.Name); err != nil {
		return err
	}
	in.Name = strings.TrimSpace(in.Name)
	if !in.Strategy.Valid() {
		return invalid("strategy", "strategy_invalid")
	}
	if in.Fallback == "" {
		in.Fallback = FallbackDirect
	}
	if !in.Fallback.Valid() {
		return invalid("fallback", "fallback_invalid")
	}
	if len(in.Members) == 0 {
		return invalid("members", "members_required")
	}
	if len(in.Members) > MaxBalancerMembers {
		return invalid("members", "members_too_many")
	}
	seen := map[string]bool{}
	for i := range in.Members {
		m := &in.Members[i]
		if seen[m.OutboundID] {
			return invalid("members", "members_duplicate")
		}
		seen[m.OutboundID] = true
		if m.Weight == 0 {
			m.Weight = 1
		}
		if m.Weight < 1 || m.Weight > MaxWeight {
			return invalid("members", "weight_invalid")
		}
		if _, err := s.GetOutbound(ctx, m.OutboundID); err != nil {
			if errors.Is(err, ErrNotFound) {
				return invalid("members", "member_unknown")
			}
			return err
		}
	}
	return nil
}

// CreateBalancer stores a new balancer.
func (s *Service) CreateBalancer(ctx context.Context, in BalancerInput) (*Balancer, error) {
	if err := s.validateBalancer(ctx, &in); err != nil {
		return nil, err
	}
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM egress_balancers`).Scan(&count); err != nil {
		return nil, fmt.Errorf("egress: count balancers: %w", err)
	}
	if count >= MaxBalancers {
		return nil, invalid("name", "balancers_full")
	}
	members, _ := json.Marshal(in.Members)
	now := s.now().UTC()
	b := &Balancer{ID: domain.NewID(), Name: in.Name, Strategy: in.Strategy, Fallback: in.Fallback,
		Members: in.Members, Enabled: in.Enabled, CreatedAt: now, UpdatedAt: now}
	_, err := s.db.ExecContext(ctx, `INSERT INTO egress_balancers
		(id, name, strategy, fallback, members_json, enabled, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		b.ID, b.Name, string(b.Strategy), string(b.Fallback), string(members), boolInt(b.Enabled), ts(now), ts(now))
	if isUnique(err) {
		return nil, invalid("name", "name_taken")
	}
	if err != nil {
		return nil, fmt.Errorf("egress: insert balancer: %w", err)
	}
	return b, nil
}

// UpdateBalancer replaces a balancer's definition.
func (s *Service) UpdateBalancer(ctx context.Context, id string, in BalancerInput) (*Balancer, error) {
	if _, err := s.GetBalancer(ctx, id); err != nil {
		return nil, err
	}
	if err := s.validateBalancer(ctx, &in); err != nil {
		return nil, err
	}
	members, _ := json.Marshal(in.Members)
	_, err := s.db.ExecContext(ctx, `UPDATE egress_balancers SET name = ?, strategy = ?, fallback = ?,
		members_json = ?, enabled = ?, updated_at = ? WHERE id = ?`,
		in.Name, string(in.Strategy), string(in.Fallback), string(members), boolInt(in.Enabled), ts(s.now()), id)
	if isUnique(err) {
		return nil, invalid("name", "name_taken")
	}
	if err != nil {
		return nil, fmt.Errorf("egress: update balancer: %w", err)
	}
	return s.GetBalancer(ctx, id)
}

// SetBalancerEnabled toggles a balancer.
func (s *Service) SetBalancerEnabled(ctx context.Context, id string, enabled bool) error {
	res, err := s.db.ExecContext(ctx, `UPDATE egress_balancers SET enabled = ?, updated_at = ? WHERE id = ?`, boolInt(enabled), ts(s.now()), id)
	if err != nil {
		return fmt.Errorf("egress: toggle balancer: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteBalancer removes an unreferenced balancer.
func (s *Service) DeleteBalancer(ctx context.Context, id string) error {
	refs, err := s.references(ctx, Target{Kind: TargetBalancer, ID: id})
	if err != nil {
		return err
	}
	if len(refs) > 0 {
		return invalidDetail("delete", "in_use", strings.Join(refs, ", "))
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM egress_balancers WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("egress: delete balancer: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// --- rules ---------------------------------------------------------------------

const ruleColumns = `id, name, position, enabled, target_kind, target_id, presets_json, matchers, created_at, updated_at`

func scanRule(scan func(...any) error) (*Rule, error) {
	var (
		r                        Rule
		enabled                  int
		kind, target, presetJSON string
		created, updated         string
	)
	if err := scan(&r.ID, &r.Name, &r.Position, &enabled, &kind, &target, &presetJSON, &r.Matchers, &created, &updated); err != nil {
		return nil, err
	}
	r.Enabled, r.Target = enabled == 1, Target{Kind: TargetKind(kind), ID: target}
	r.CreatedAt, r.UpdatedAt = parseTS(created), parseTS(updated)
	if err := json.Unmarshal([]byte(presetJSON), &r.Presets); err != nil {
		return nil, fmt.Errorf("egress: stored rule %s presets: %w", r.ID, err)
	}
	return &r, nil
}

// ListRules returns every rule in evaluation order.
func (s *Service) ListRules(ctx context.Context) ([]*Rule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+ruleColumns+` FROM egress_rules ORDER BY position, created_at`)
	if err != nil {
		return nil, fmt.Errorf("egress: list rules: %w", err)
	}
	defer rows.Close()
	var out []*Rule
	for rows.Next() {
		r, err := scanRule(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetRule returns one rule.
func (s *Service) GetRule(ctx context.Context, id string) (*Rule, error) {
	r, err := scanRule(s.db.QueryRowContext(ctx, `SELECT `+ruleColumns+` FROM egress_rules WHERE id = ?`, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("egress: get rule: %w", err)
	}
	return r, nil
}

// RuleInput creates or replaces a rule.
type RuleInput struct {
	Name     string
	Target   Target
	Presets  []string
	Matchers string
	Enabled  bool
}

func (s *Service) validateRule(ctx context.Context, in *RuleInput) error {
	if err := validName(in.Name); err != nil {
		return err
	}
	in.Name = strings.TrimSpace(in.Name)
	if err := s.validateTarget(ctx, in.Target, true); err != nil {
		return err
	}
	clean := make([]string, 0, len(in.Presets))
	seen := map[string]bool{}
	for _, id := range in.Presets {
		if _, ok := PresetByID(id); !ok {
			return invalid("presets", "preset_unknown")
		}
		if !seen[id] {
			seen[id] = true
			clean = append(clean, id)
		}
	}
	sort.Strings(clean)
	in.Presets = clean
	in.Matchers = strings.TrimSpace(strings.ReplaceAll(in.Matchers, "\r\n", "\n"))
	matchers, err := ParseMatchers(in.Matchers)
	if err != nil {
		return err
	}
	if len(matchers) == 0 && len(in.Presets) == 0 {
		return invalid("matchers", "rule_empty")
	}
	return nil
}

// validateTarget checks that a target exists. allowBlock is false for the
// default route (blocking everything is never the intent).
func (s *Service) validateTarget(ctx context.Context, t Target, allowBlock bool) error {
	switch t.Kind {
	case TargetDirect:
		return nil
	case TargetBlock:
		if !allowBlock {
			return invalid("target", "target_invalid")
		}
		return nil
	case TargetOutbound:
		if _, err := s.GetOutbound(ctx, t.ID); err != nil {
			if errors.Is(err, ErrNotFound) {
				return invalid("target", "target_unknown")
			}
			return err
		}
		return nil
	case TargetBalancer:
		if _, err := s.GetBalancer(ctx, t.ID); err != nil {
			if errors.Is(err, ErrNotFound) {
				return invalid("target", "target_unknown")
			}
			return err
		}
		return nil
	}
	return invalid("target", "target_invalid")
}

// CreateRule appends a rule after the existing ones.
func (s *Service) CreateRule(ctx context.Context, in RuleInput) (*Rule, error) {
	if err := s.validateRule(ctx, &in); err != nil {
		return nil, err
	}
	presets, _ := json.Marshal(in.Presets)
	now := s.now().UTC()
	r := &Rule{ID: domain.NewID(), Name: in.Name, Enabled: in.Enabled, Target: in.Target,
		Presets: in.Presets, Matchers: in.Matchers, CreatedAt: now, UpdatedAt: now}
	err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
		var count, last int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(MAX(position), 0) FROM egress_rules`).Scan(&count, &last); err != nil {
			return err
		}
		if count >= MaxRules {
			return invalid("name", "rules_full")
		}
		r.Position = last + 1
		_, err := tx.ExecContext(ctx, `INSERT INTO egress_rules (`+ruleColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			r.ID, r.Name, r.Position, boolInt(r.Enabled), string(r.Target.Kind), r.Target.ID, string(presets), r.Matchers, ts(now), ts(now))
		return err
	})
	if err != nil {
		if _, ok := AsValidation(err); ok {
			return nil, err
		}
		return nil, fmt.Errorf("egress: insert rule: %w", err)
	}
	return r, nil
}

// UpdateRule replaces a rule's definition, keeping its position.
func (s *Service) UpdateRule(ctx context.Context, id string, in RuleInput) (*Rule, error) {
	if _, err := s.GetRule(ctx, id); err != nil {
		return nil, err
	}
	if err := s.validateRule(ctx, &in); err != nil {
		return nil, err
	}
	presets, _ := json.Marshal(in.Presets)
	if _, err := s.db.ExecContext(ctx, `UPDATE egress_rules SET name = ?, enabled = ?, target_kind = ?, target_id = ?,
		presets_json = ?, matchers = ?, updated_at = ? WHERE id = ?`,
		in.Name, boolInt(in.Enabled), string(in.Target.Kind), in.Target.ID, string(presets), in.Matchers, ts(s.now()), id); err != nil {
		return nil, fmt.Errorf("egress: update rule: %w", err)
	}
	return s.GetRule(ctx, id)
}

// SetRuleEnabled toggles a rule.
func (s *Service) SetRuleEnabled(ctx context.Context, id string, enabled bool) error {
	res, err := s.db.ExecContext(ctx, `UPDATE egress_rules SET enabled = ?, updated_at = ? WHERE id = ?`, boolInt(enabled), ts(s.now()), id)
	if err != nil {
		return fmt.Errorf("egress: toggle rule: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// MoveRule swaps a rule with its neighbour (delta -1 = earlier, +1 = later).
func (s *Service) MoveRule(ctx context.Context, id string, delta int) error {
	if delta != -1 && delta != 1 {
		return invalid("position", "position_invalid")
	}
	return s.db.WithTx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id FROM egress_rules ORDER BY position, created_at`)
		if err != nil {
			return err
		}
		var ids []string
		for rows.Next() {
			var v string
			if err := rows.Scan(&v); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, v)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		at := -1
		for i, v := range ids {
			if v == id {
				at = i
			}
		}
		if at < 0 {
			return ErrNotFound
		}
		other := at + delta
		if other < 0 || other >= len(ids) {
			return nil // already first/last
		}
		ids[at], ids[other] = ids[other], ids[at]
		for i, v := range ids {
			if _, err := tx.ExecContext(ctx, `UPDATE egress_rules SET position = ? WHERE id = ?`, i+1, v); err != nil {
				return err
			}
		}
		return nil
	})
}

// DeleteRule removes a rule.
func (s *Service) DeleteRule(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM egress_rules WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("egress: delete rule: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// --- settings ------------------------------------------------------------------

// GetSettings returns the routing options.
func (s *Service) GetSettings(ctx context.Context) (Settings, error) {
	var (
		st       Settings
		kind, id string
		blockDNS int
		updated  string
	)
	err := s.db.QueryRowContext(ctx, `SELECT default_target_kind, default_target_id, block_encrypted_dns,
		geosite_url, geoip_url, updated_at FROM egress_settings WHERE id = 1`).
		Scan(&kind, &id, &blockDNS, &st.GeositeURL, &st.GeoIPURL, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Settings{DefaultTarget: Direct, BlockEncryptedDNS: true}, nil
	}
	if err != nil {
		return Settings{}, fmt.Errorf("egress: settings: %w", err)
	}
	st.DefaultTarget = Target{Kind: TargetKind(kind), ID: id}
	st.BlockEncryptedDNS = blockDNS == 1
	st.UpdatedAt = parseTS(updated)
	return st, nil
}

// UpdateSettings validates and stores the routing options.
func (s *Service) UpdateSettings(ctx context.Context, st Settings) error {
	if err := s.validateTarget(ctx, st.DefaultTarget, false); err != nil {
		return err
	}
	for field, raw := range map[string]*string{"geosite_url": &st.GeositeURL, "geoip_url": &st.GeoIPURL} {
		v := strings.TrimSpace(*raw)
		if v != "" {
			u, err := url.Parse(v)
			if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || len(v) > 512 {
				return invalid(field, "url_invalid")
			}
		}
		*raw = v
	}
	_, err := s.db.ExecContext(ctx, `UPDATE egress_settings SET default_target_kind = ?, default_target_id = ?,
		block_encrypted_dns = ?, geosite_url = ?, geoip_url = ?, updated_at = ? WHERE id = 1`,
		string(st.DefaultTarget.Kind), st.DefaultTarget.ID, boolInt(st.BlockEncryptedDNS), st.GeositeURL, st.GeoIPURL, ts(s.now()))
	if err != nil {
		return fmt.Errorf("egress: update settings: %w", err)
	}
	return nil
}

// --- runtime snapshot ------------------------------------------------------------

// OutboundState is an outbound with its decrypted secret, for runtime use only.
type OutboundState struct {
	*Outbound
	Secret Secret
}

// Snapshot is the complete desired state the routing runtime renders.
type Snapshot struct {
	Outbounds []OutboundState
	Balancers []*Balancer
	Rules     []*Rule
	Settings  Settings
}

// Empty reports whether egress routing has nothing to render.
func (s *Snapshot) Empty() bool {
	return len(s.Outbounds) == 0 && len(s.Rules) == 0 && s.Settings.DefaultTarget.Kind == TargetDirect
}

// LoadSnapshot reads the desired state, decrypting outbound secrets.
func (s *Service) LoadSnapshot(ctx context.Context) (*Snapshot, error) {
	snap := &Snapshot{}
	outbounds, err := s.ListOutbounds(ctx)
	if err != nil {
		return nil, err
	}
	for _, o := range outbounds {
		sec, err := s.OutboundSecret(ctx, o.ID)
		if err != nil {
			return nil, err
		}
		snap.Outbounds = append(snap.Outbounds, OutboundState{Outbound: o, Secret: sec})
	}
	if snap.Balancers, err = s.ListBalancers(ctx); err != nil {
		return nil, err
	}
	if snap.Rules, err = s.ListRules(ctx); err != nil {
		return nil, err
	}
	if snap.Settings, err = s.GetSettings(ctx); err != nil {
		return nil, err
	}
	return snap, nil
}
