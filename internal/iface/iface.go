// Package iface manages tunnel interfaces/profiles (`awg0…awgN`): ports,
// IPv4 pools, MTU, and the one obfuscation profile per interface (ADR-0002).
// Every parameter is validated against the kernel-README constraint set
// pinned in docs/integrations/amneziawg.md — the AWG tools parser accepts
// invalid combinations and the daemon rejects them at setconf time, so
// WG-Guard must validate before anything reaches the backend.
package iface

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
	"github.com/Sir-Adnan/wg-guard/internal/settings"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel"
)

// Interface is a stored tunnel interface/profile.
type Interface struct {
	ID               string
	Name             string // awgN
	ListenPort       int
	Subnet           string // CIDR
	MTU              int
	PublicKey        string // base64 server key (client configs embed it)
	PrivKeyEnc       []byte // AES-GCM envelope
	Obfuscation      Obfuscation
	Preset           string
	Enabled          bool
	BackendMode      domain.BackendMode
	EndpointOverride string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Obfuscation mirrors the pinned parameter set: legacy 1.0 (Jc/Jmin/Jmax/
// S1/S2/H1–H4 + optional I1–I5, verified end-to-end) plus the capability-gated
// 2.0/3.x fields (formats verified against the pinned tools src/config.c;
// runtime verification pending the Phase 8 VPS matrix — amneziawg.md).
// Zero-value with Enabled=false is the "plain WG" profile.
type Obfuscation struct {
	Enabled            bool
	Jc                 int
	Jmin, Jmax         int
	S1, S2             int
	H1, H2, H3, H4     uint32
	I1, I2, I3, I4, I5 string

	S3, S4                 int    // plain u16 when set
	HeaderProtectionKey    string // base64 32-byte key, "" = disabled
	ContentPaddingAddition string // "N" or "N-M" (u16 bounds), "" = disabled
	RekeyAfterTime         string // seconds, "N" or "N-M", "" = upstream default
	RekeyTimeout           string
	RejectAfterTime        string
	KeepaliveTimeout       string
	MaxHandshakeAttempts   string
	RandomTrailers         bool
	DisableCookies         bool
}

// Constraints from the AmneziaWG kernel-README (docs/integrations/amneziawg.md).
const (
	JcMax             = 128
	JmaxMax           = 1280
	S1Max             = 1132
	S2Max             = 1188
	MinUsefulPoolBits = 29 // smaller pools cannot host meaningful peer counts
)

var nameRe = regexp.MustCompile(`^awg([0-9]{1,3})$`)

// rangeRe matches the u16-range form the pinned tools parser accepts
// ("N" or "N-M", verified: u16_range_from_string in src/config.c).
var rangeRe = regexp.MustCompile(`^([0-9]{1,5})(-([0-9]{1,5}))?$`)

// validateRange checks an "N" or "N-M" string within u16 bounds.
func validateRange(field, v string) error {
	m := rangeRe.FindStringSubmatch(v)
	if m == nil {
		return domain.E(domain.CodeParamConstraint, `%s must be "N" or "low-high" (u16), got %q`, field, v)
	}
	lo, _ := strconv.Atoi(m[1])
	if lo > 65535 {
		return domain.E(domain.CodeParamConstraint, "%s exceeds the u16 bound (65535)", field)
	}
	if m[3] != "" {
		hi, _ := strconv.Atoi(m[3])
		if hi > 65535 || hi < lo {
			return domain.E(domain.CodeParamConstraint, `%s range %q is invalid (low <= high <= 65535)`, field, v)
		}
	}
	return nil
}

// ValidateObfuscation enforces the constraint set. A zero params struct with
// Enabled=false is the plain-WG profile; Enabled=true requires a complete,
// constraint-clean parameter set (no partial profiles).
func ValidateObfuscation(o Obfuscation) error {
	if !o.Enabled {
		if o.Jc != 0 || o.Jmin != 0 || o.Jmax != 0 || o.S1 != 0 || o.S2 != 0 ||
			o.H1 != 0 || o.H2 != 0 || o.H3 != 0 || o.H4 != 0 ||
			o.S3 != 0 || o.S4 != 0 || o.HeaderProtectionKey != "" ||
			o.ContentPaddingAddition != "" || o.RekeyAfterTime != "" || o.RekeyTimeout != "" ||
			o.RejectAfterTime != "" || o.KeepaliveTimeout != "" || o.MaxHandshakeAttempts != "" ||
			o.RandomTrailers || o.DisableCookies {
			return domain.E(domain.CodeParamConstraint, "obfuscation disabled but parameters are set")
		}
		return nil
	}
	// Capability-gated 2.0/3.x parameters (formats verified from the pinned
	// tools source; kernel-module acceptance verified on a real VPS —
	// docs/integrations/amneziawg.md).
	if o.S3 < 0 || o.S3 > 65535 || o.S4 < 0 || o.S4 > 65535 {
		return domain.E(domain.CodeParamConstraint, "S3/S4 must be 0–65535 (0 = unset)")
	}
	if o.HeaderProtectionKey != "" {
		k, err := base64.StdEncoding.DecodeString(o.HeaderProtectionKey)
		if err != nil || len(k) != 32 {
			return domain.E(domain.CodeParamConstraint, "HeaderProtectionKey must be a base64-encoded 32-byte key")
		}
		// Kernel-enforced coupling (verified on VPS): HPK is rejected unless
		// S3 AND S4 are non-zero in the same setconf message.
		if o.S3 == 0 || o.S4 == 0 {
			return domain.E(domain.CodeParamConstraint,
				"HeaderProtectionKey requires S3 and S4 to be set (kernel constraint)")
		}
	}
	for _, rv := range []struct{ name, val string }{
		{"ContentPaddingAddition", o.ContentPaddingAddition},
		{"RekeyAfterTime", o.RekeyAfterTime},
		{"RekeyTimeout", o.RekeyTimeout},
		{"RejectAfterTime", o.RejectAfterTime},
		{"KeepaliveTimeout", o.KeepaliveTimeout},
		{"MaxHandshakeAttempts", o.MaxHandshakeAttempts},
	} {
		if rv.val == "" {
			continue
		}
		if err := validateRange(rv.name, rv.val); err != nil {
			return err
		}
	}
	if o.Jc < 1 || o.Jc > JcMax {
		return domain.E(domain.CodeParamConstraint, "Jc must be 1–%d (recommended 4–12), got %d", JcMax, o.Jc)
	}
	if o.Jmin >= o.Jmax {
		return domain.E(domain.CodeParamConstraint, "Jmin (%d) must be less than Jmax (%d)", o.Jmin, o.Jmax)
	}
	if o.Jmax > JmaxMax {
		return domain.E(domain.CodeParamConstraint, "Jmax must be ≤ %d, got %d", JmaxMax, o.Jmax)
	}
	if o.S1 < 0 || o.S1 > S1Max {
		return domain.E(domain.CodeParamConstraint, "S1 must be 0–%d, got %d", S1Max, o.S1)
	}
	if o.S2 < 0 || o.S2 > S2Max {
		return domain.E(domain.CodeParamConstraint, "S2 must be 0–%d, got %d", S2Max, o.S2)
	}
	if o.S1+56 == o.S2 {
		return domain.E(domain.CodeParamConstraint, "S1+56 must not equal S2 (%d+56=%d)", o.S1, o.S2)
	}
	hs := [4]uint32{o.H1, o.H2, o.H3, o.H4}
	for i := 0; i < 4; i++ {
		if hs[i] == 0 {
			return domain.E(domain.CodeParamConstraint, "H1–H4 must all be set (non-zero) when obfuscation is enabled")
		}
		for j := i + 1; j < 4; j++ {
			if hs[i] == hs[j] {
				return domain.E(domain.CodeParamConstraint, "H1–H4 must be pairwise distinct (H%d == H%d)", i+1, j+1)
			}
		}
	}
	return nil
}

// randomizeHeaders fills unset H1-H4 with crypto/rand values - pairwise
// distinct and non-zero (the runtime rejects duplicates/zero at setconf).
// Strong magic headers are per-profile secrets; the shipped presets no longer
// hardcode them. Set headers are left untouched.
func randomizeHeaders(o *Obfuscation) {
	if !o.Enabled {
		return
	}
	hs := [4]*uint32{&o.H1, &o.H2, &o.H3, &o.H4}
	used := map[uint32]bool{}
	for _, h := range hs {
		if *h != 0 {
			used[*h] = true
		}
	}
	for _, h := range hs {
		if *h != 0 {
			continue
		}
		var b [4]byte
		for {
			if _, err := rand.Read(b[:]); err != nil {
				return // keep any remaining zeros; Create validates the result
			}
			v := binary.LittleEndian.Uint32(b[:])
			if v != 0 && !used[v] {
				*h = v
				used[v] = true
				break
			}
		}
	}
}

// ValidatePortRange enforces the registry-driven random allocation window.
func ValidatePortRange(min, max int) error {
	if min < 1024 || min > 65535 || max < 1024 || max > 65535 {
		return domain.E(domain.CodeSettingInvalid, "port range must be within 1024–65535")
	}
	if min > max {
		return domain.E(domain.CodeSettingInvalid, "network.port_min (%d) must be ≤ network.port_max (%d)", min, max)
	}
	return nil
}

// Service wires the interface service to its dependencies. The key ring
// encrypts the interface private key at rest (never stored plaintext).
type Service struct {
	db   *database.DB
	reg  *settings.Registry
	ring *secrets.KeyRing
	now  func() time.Time
}

func NewService(db *database.DB, reg *settings.Registry, ring *secrets.KeyRing) *Service {
	return &Service{db: db, reg: reg, ring: ring, now: time.Now}
}

// CreateInput is a validated-at-the-edge request; all rules run here.
type CreateInput struct {
	Name             string
	ListenPort       int    // 0 → allocate randomly from network.port_min..port_max
	Subnet           string // CIDR; "" → default pool for the name (10.8.N.0/24)
	MTU              int    // 0 → network.mtu setting
	Obfuscation      Obfuscation
	Preset           string
	BackendMode      domain.BackendMode
	EndpointOverride string
}

// Create validates and persists a new profile. It does not touch the tunnel
// backend — the tunnel manager (Phase 2 wiring) reconciles the backend from
// the DB (overview.md: DB is the source of truth).
func (s *Service) Create(ctx context.Context, in CreateInput) (*Interface, error) {
	m := nameRe.FindStringSubmatch(in.Name)
	if m == nil {
		return nil, domain.E(domain.CodeInvalidRequest, "interface name must be awgN (e.g. awg0)")
	}
	n, _ := strconv.Atoi(m[1])
	maxCount, err := s.reg.GetInt(ctx, "interfaces.max_count")
	if err != nil {
		return nil, err
	}
	if n >= maxCount {
		return nil, domain.E(domain.CodeInvalidRequest, "interface index %d is at or above the configured cap (%d, interfaces.max_count)", n, maxCount)
	}

	backendMode := in.BackendMode
	if backendMode == "" {
		backendMode = domain.BackendKernel
	}
	if !backendMode.Valid() {
		return nil, domain.E(domain.CodeInvalidRequest, "backend mode %q is not kernel|userspace", backendMode)
	}

	port := in.ListenPort
	if port == 0 {
		port, err = s.allocatePort(ctx)
		if err != nil {
			return nil, err
		}
	} else if err := validatePort(port); err != nil {
		return nil, err
	}

	subnet := in.Subnet
	if subnet == "" {
		subnet = s.defaultPool(ctx, n)
	}
	if err := settings.ValidSubnet(subnet); err != nil {
		return nil, domain.E(domain.CodeSubnetInvalid, "%v", err)
	}
	prefix := netip.MustParsePrefix(subnet)
	if err := validatePoolSize(prefix); err != nil {
		return nil, err
	}

	mtu := in.MTU
	if mtu == 0 {
		mtu, err = s.reg.GetInt(ctx, "network.mtu")
		if err != nil {
			return nil, err
		}
	}
	if mtu < 576 || mtu > 65535 {
		return nil, domain.E(domain.CodeInvalidRequest, "MTU must be 576–65535, got %d", mtu)
	}

	randomizeHeaders(&in.Obfuscation)
	if err := ValidateObfuscation(in.Obfuscation); err != nil {
		return nil, err
	}

	// Server keypair: standard Curve25519 (tunnel.GenerateKeyPair); the
	// private key is stored AES-GCM-encrypted with the master key.
	kp, err := tunnel.GenerateKeyPair()
	if err != nil {
		return nil, err
	}
	privEnc, err := s.ring.Encrypt([]byte(kp.Private))
	if err != nil {
		return nil, fmt.Errorf("iface: encrypt private key: %w", err)
	}

	ifc := &Interface{
		ID:               domain.NewID(),
		Name:             in.Name,
		ListenPort:       port,
		Subnet:           subnet,
		MTU:              mtu,
		PublicKey:        kp.Public,
		PrivKeyEnc:       privEnc,
		Obfuscation:      in.Obfuscation,
		Preset:           in.Preset,
		Enabled:          true,
		BackendMode:      backendMode,
		EndpointOverride: strings.TrimSpace(in.EndpointOverride),
		CreatedAt:        s.now().UTC(),
		UpdatedAt:        s.now().UTC(),
	}

	err = s.db.WithTx(ctx, func(tx *sql.Tx) error {
		return s.insertWithChecks(ctx, tx, ifc)
	})
	if err != nil {
		return nil, err
	}
	return ifc, nil
}

// insertWithChecks performs the uniqueness/overlap checks and insert inside
// one immediate transaction.
func (s *Service) insertWithChecks(ctx context.Context, tx *sql.Tx, ifc *Interface) error {
	var name string
	err := tx.QueryRowContext(ctx, `SELECT name FROM tunnel_interfaces WHERE name = ?`, ifc.Name).Scan(&name)
	if err == nil {
		return domain.E(domain.CodeInterfaceNameTaken, "interface %s already exists", ifc.Name)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("iface: name check: %w", err)
	}
	var port int
	err = tx.QueryRowContext(ctx, `SELECT listen_port FROM tunnel_interfaces WHERE listen_port = ?`, ifc.ListenPort).Scan(&port)
	if err == nil {
		return domain.E(domain.CodePortInUse, "listen port %d is already in use", ifc.ListenPort)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("iface: port check: %w", err)
	}
	if err := s.checkOverlaps(ctx, tx, ifc.ID, ifc.Subnet); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO tunnel_interfaces
		(id, name, listen_port, ipv4_subnet, mtu, public_key, private_key_encrypted,
		 jc, jmin, jmax, s1, s2, h1, h2, h3, h4,
		 i1, i2, i3, i4, i5, preset_name, enabled, backend_mode, endpoint_override, created_at, updated_at,
		 s3, s4, header_protection_key, content_padding_addition, rekey_after_time,
		 rekey_timeout, reject_after_time, keepalive_timeout, max_handshake_attempts,
		 random_trailers, disable_cookies)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
			?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ifc.ID, ifc.Name, ifc.ListenPort, ifc.Subnet, ifc.MTU, ifc.PublicKey, ifc.PrivKeyEnc,
		nullInt(ifc.Obfuscation.Jc, ifc.Obfuscation.Enabled), nullInt(ifc.Obfuscation.Jmin, ifc.Obfuscation.Enabled),
		nullInt(ifc.Obfuscation.Jmax, ifc.Obfuscation.Enabled), nullInt(ifc.Obfuscation.S1, ifc.Obfuscation.Enabled),
		nullInt(ifc.Obfuscation.S2, ifc.Obfuscation.Enabled),
		nullU32(ifc.Obfuscation.H1, ifc.Obfuscation.Enabled), nullU32(ifc.Obfuscation.H2, ifc.Obfuscation.Enabled),
		nullU32(ifc.Obfuscation.H3, ifc.Obfuscation.Enabled), nullU32(ifc.Obfuscation.H4, ifc.Obfuscation.Enabled),
		nullText(ifc.Obfuscation.I1), nullText(ifc.Obfuscation.I2), nullText(ifc.Obfuscation.I3),
		nullText(ifc.Obfuscation.I4), nullText(ifc.Obfuscation.I5),
		ifc.Preset, boolInt(ifc.Enabled), string(ifc.BackendMode),
		nullText(ifc.EndpointOverride),
		ifc.CreatedAt.Format(time.RFC3339Nano), ifc.UpdatedAt.Format(time.RFC3339Nano),
		ifc.Obfuscation.S3, ifc.Obfuscation.S4,
		ifc.Obfuscation.HeaderProtectionKey, ifc.Obfuscation.ContentPaddingAddition,
		ifc.Obfuscation.RekeyAfterTime, ifc.Obfuscation.RekeyTimeout,
		ifc.Obfuscation.RejectAfterTime, ifc.Obfuscation.KeepaliveTimeout,
		ifc.Obfuscation.MaxHandshakeAttempts,
		boolInt(ifc.Obfuscation.RandomTrailers), boolInt(ifc.Obfuscation.DisableCookies))
	if err != nil {
		if isUnique(err) {
			return domain.E(domain.CodeInterfaceNameTaken, "interface %s already exists", ifc.Name)
		}
		return fmt.Errorf("iface: insert: %w", err)
	}
	return nil
}

// checkOverlaps rejects pools overlapping another interface's pool or common
// host-network ranges (RFC1918 overlap among pools is the dangerous case; a
// pool inside the host's own subnet breaks routing).
func (s *Service) checkOverlaps(ctx context.Context, tx *sql.Tx, excludeID, subnet string) error {
	rows, err := tx.QueryContext(ctx, `SELECT id, name, ipv4_subnet FROM tunnel_interfaces WHERE id != ?`, excludeID)
	if err != nil {
		return fmt.Errorf("iface: overlap scan: %w", err)
	}
	defer rows.Close()
	prefix := netip.MustParsePrefix(subnet)
	for rows.Next() {
		var id, name, other string
		if err := rows.Scan(&id, &name, &other); err != nil {
			return fmt.Errorf("iface: overlap scan: %w", err)
		}
		otherPrefix, err := netip.ParsePrefix(other)
		if err != nil {
			continue // unparseable legacy row: leave it alone
		}
		if prefix.Overlaps(otherPrefix) {
			return domain.E(domain.CodeSubnetOverlap, "pool %s overlaps interface %s pool %s", subnet, name, other)
		}
	}
	return rows.Err()
}

func validatePoolSize(p netip.Prefix) error {
	if p.Bits() > MinUsefulPoolBits {
		return domain.E(domain.CodeSubnetInvalid, "pool /%d is too small (minimum /%d)", p.Bits(), MinUsefulPoolBits)
	}
	return nil
}

func validatePort(p int) error {
	if p < 1024 || p > 65535 {
		return domain.E(domain.CodeInvalidRequest, "listen port must be 1024–65535, got %d", p)
	}
	return nil
}

// defaultPool resolves the subnet for a new interface whose subnet was left
// blank: awg0 honors the network.default_pool setting (seeded by the
// installer or set in the panel to avoid a conflicting 10.8.0.0/24); later
// interfaces continue the 10.8.N.0/24 ladder (requirements.md).
func (s *Service) defaultPool(ctx context.Context, n int) string {
	if n == 0 {
		if pool, err := s.reg.GetString(ctx, "network.default_pool"); err == nil && pool != "" {
			return pool
		}
	}
	return fmt.Sprintf("10.8.%d.0/24", n)
}

// allocatePort picks a random free port from the configured window.
func (s *Service) allocatePort(ctx context.Context) (int, error) {
	min, err := s.reg.GetInt(ctx, "network.port_min")
	if err != nil {
		return 0, err
	}
	max, err := s.reg.GetInt(ctx, "network.port_max")
	if err != nil {
		return 0, err
	}
	if err := ValidatePortRange(min, max); err != nil {
		return 0, err
	}
	used := map[int]bool{}
	rows, err := s.db.QueryContext(ctx, `SELECT listen_port FROM tunnel_interfaces`)
	if err != nil {
		return 0, fmt.Errorf("iface: port scan: %w", err)
	}
	for rows.Next() {
		var p int
		if err := rows.Scan(&p); err != nil {
			rows.Close()
			return 0, fmt.Errorf("iface: port scan: %w", err)
		}
		used[p] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iface: port scan: %w", err)
	}
	// Deterministic scan with a pseudo-random start: crypto/rand is not
	// needed here (a port is not a secret); the scan guarantees freedom.
	span := max - min + 1
	start := int(time.Now().UnixNano() % int64(span))
	for i := 0; i < span; i++ {
		p := min + (start+i)%span
		if !used[p] {
			return p, nil
		}
	}
	return 0, domain.E(domain.CodePortInUse, "no free port in range %d–%d", min, max)
}

// Get loads one interface by ID.
func (s *Service) Get(ctx context.Context, id string) (*Interface, error) {
	return s.getBy(ctx, `id`, id)
}

// GetByName loads one interface by name (awgN).
func (s *Service) GetByName(ctx context.Context, name string) (*Interface, error) {
	return s.getBy(ctx, `name`, name)
}

func (s *Service) getBy(ctx context.Context, col, val string) (*Interface, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+ifaceColumns+` FROM tunnel_interfaces WHERE `+col+` = ?`, val)
	ifc, err := scanIface(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.E(domain.CodeInterfaceNotFound, "interface %q not found", val)
	}
	if err != nil {
		return nil, fmt.Errorf("iface: get: %w", err)
	}
	return ifc, nil
}

// ReencryptSecrets rotates interface private-key envelopes (master-key
// rotation carrier). Collect-then-update like the device carrier: rotation
// order in secrets.Rotate guarantees both keys stay available.
func (s *Service) ReencryptSecrets(from, to *secrets.Cipher) error {
	rows, err := s.db.Query(`SELECT id, private_key_encrypted FROM tunnel_interfaces`)
	if err != nil {
		return fmt.Errorf("iface: rotate scan: %w", err)
	}
	type row struct {
		id string
		pk []byte
	}
	var updates []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.pk); err != nil {
			rows.Close()
			return fmt.Errorf("iface: rotate: %w", err)
		}
		pt, err := from.Decrypt(r.pk)
		if err != nil {
			rows.Close()
			return fmt.Errorf("iface: rotate %s: %w", r.id, err)
		}
		r.pk, err = to.Encrypt(pt)
		if err != nil {
			rows.Close()
			return fmt.Errorf("iface: rotate %s: %w", r.id, err)
		}
		updates = append(updates, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iface: rotate: %w", err)
	}
	for _, r := range updates {
		if _, err := s.db.Exec(`UPDATE tunnel_interfaces SET private_key_encrypted = ? WHERE id = ?`,
			r.pk, r.id); err != nil {
			return fmt.Errorf("iface: rotate update %s: %w", r.id, err)
		}
	}
	return nil
}

// PrivateKey decrypts the interface private key (config rendering and peer
// apply only — never logged, never returned by API handlers).
func (s *Service) PrivateKey(ifc *Interface) (string, error) {
	pt, err := s.ring.Decrypt(ifc.PrivKeyEnc)
	if err != nil {
		return "", fmt.Errorf("iface: decrypt private key: %w", err)
	}
	return string(pt), nil
}

const ifaceColumns = `id, name, listen_port, ipv4_subnet, mtu, public_key, private_key_encrypted,
	jc, jmin, jmax, s1, s2,
	h1, h2, h3, h4, i1, i2, i3, i4, i5, preset_name, enabled, backend_mode,
	endpoint_override, created_at, updated_at,
	s3, s4, header_protection_key, content_padding_addition,
	rekey_after_time, rekey_timeout, reject_after_time, keepalive_timeout,
	max_handshake_attempts, random_trailers, disable_cookies`

type rowScanner interface{ Scan(dest ...any) error }

func scanIface(row rowScanner) (*Interface, error) {
	var (
		ifc                    Interface
		preset                 string
		enabled                int
		mode                   string
		createdStr             string
		updatedStr             string
		jc, jmin, jmax, s1, s2 sql.NullInt64
		h1, h2, h3, h4         sql.NullInt64
		i1, i2, i3, i4, i5     sql.NullString
		endpoint               sql.NullString
		s3, s4                 sql.NullInt64
		hpk                    sql.NullString
		padding                sql.NullString
		rekeyAfter             sql.NullString
		rekeyTimeout           sql.NullString
		rejectAfter            sql.NullString
		keepaliveTimeout       sql.NullString
		maxHandshake           sql.NullString
		randomTrailers         sql.NullInt64
		disableCookies         sql.NullInt64
	)
	err := row.Scan(&ifc.ID, &ifc.Name, &ifc.ListenPort, &ifc.Subnet, &ifc.MTU,
		&ifc.PublicKey, &ifc.PrivKeyEnc,
		&jc, &jmin, &jmax, &s1, &s2, &h1, &h2, &h3, &h4,
		&i1, &i2, &i3, &i4, &i5, &preset, &enabled, &mode, &endpoint,
		&createdStr, &updatedStr,
		&s3, &s4, &hpk, &padding, &rekeyAfter, &rekeyTimeout, &rejectAfter,
		&keepaliveTimeout, &maxHandshake, &randomTrailers, &disableCookies)
	if err != nil {
		return nil, err
	}
	ifc.Obfuscation = Obfuscation{
		Enabled: jc.Valid,
		Jc:      int(jc.Int64), Jmin: int(jmin.Int64), Jmax: int(jmax.Int64),
		S1: int(s1.Int64), S2: int(s2.Int64),
		H1: uint32(h1.Int64), H2: uint32(h2.Int64), H3: uint32(h3.Int64), H4: uint32(h4.Int64),
		I1: i1.String, I2: i2.String, I3: i3.String, I4: i4.String, I5: i5.String,
		S3: int(s3.Int64), S4: int(s4.Int64),
		HeaderProtectionKey:    hpk.String,
		ContentPaddingAddition: padding.String,
		RekeyAfterTime:         rekeyAfter.String,
		RekeyTimeout:           rekeyTimeout.String,
		RejectAfterTime:        rejectAfter.String,
		KeepaliveTimeout:       keepaliveTimeout.String,
		MaxHandshakeAttempts:   maxHandshake.String,
		RandomTrailers:         randomTrailers.Int64 == 1,
		DisableCookies:         disableCookies.Int64 == 1,
	}
	ifc.Preset = preset
	ifc.Enabled = enabled == 1
	ifc.BackendMode = domain.BackendMode(mode)
	ifc.EndpointOverride = endpoint.String
	ifc.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdStr)
	ifc.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedStr)
	return &ifc, nil
}

// List returns every interface ordered by name.
func (s *Service) List(ctx context.Context) ([]*Interface, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+ifaceColumns+` FROM tunnel_interfaces ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("iface: list: %w", err)
	}
	defer rows.Close()
	var out []*Interface
	for rows.Next() {
		ifc, err := scanIface(rows)
		if err != nil {
			return nil, fmt.Errorf("iface: scan: %w", err)
		}
		out = append(out, ifc)
	}
	return out, rows.Err()
}

// SetEnabled toggles a profile (disabled profiles are removed from the
// backend by reconciliation).
func (s *Service) SetEnabled(ctx context.Context, id string, enabled bool) error {
	res, err := s.db.ExecContext(ctx, `UPDATE tunnel_interfaces SET enabled = ?, updated_at = ? WHERE id = ?`,
		boolInt(enabled), s.now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("iface: set enabled: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.E(domain.CodeInterfaceNotFound, "interface %s not found", id)
	}
	return nil
}

// UpdateMTU changes the MTU of one profile (recommended default lives in
// settings; per-interface values override it).
func (s *Service) UpdateMTU(ctx context.Context, id string, mtu int) error {
	if mtu < 576 || mtu > 65535 {
		return domain.E(domain.CodeInvalidRequest, "MTU must be 576–65535, got %d", mtu)
	}
	res, err := s.db.ExecContext(ctx, `UPDATE tunnel_interfaces SET mtu = ?, updated_at = ? WHERE id = ?`,
		mtu, s.now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("iface: set mtu: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.E(domain.CodeInterfaceNotFound, "interface %s not found", id)
	}
	return nil
}

// UpdateInput is a partial profile update. The name, port, and pool are
// deliberately immutable here: moving an interface's port happens through
// reconcile, and a pool change would orphan allocated device addresses.
// Obfuscation-mode changes (plain ↔ obfuscated) are supported — the
// reconcile engine recreates the link for that transition (pinned fact:
// setconf cannot switch modes in place).
type UpdateInput struct {
	MTU              *int
	Enabled          *bool
	EndpointOverride *string
	Obfuscation      *Obfuscation
}

// Update applies a partial profile change and returns the updated row.
func (s *Service) Update(ctx context.Context, id string, in UpdateInput) (*Interface, error) {
	ifc, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if in.MTU != nil {
		if *in.MTU < 576 || *in.MTU > 65535 {
			return nil, domain.E(domain.CodeInvalidRequest, "MTU must be 576–65535, got %d", *in.MTU)
		}
		ifc.MTU = *in.MTU
	}
	if in.Enabled != nil {
		ifc.Enabled = *in.Enabled
	}
	if in.EndpointOverride != nil {
		ifc.EndpointOverride = strings.TrimSpace(*in.EndpointOverride)
	}
	if in.Obfuscation != nil {
		if err := ValidateObfuscation(*in.Obfuscation); err != nil {
			return nil, err
		}
		ifc.Obfuscation = *in.Obfuscation
	}
	ifc.UpdatedAt = s.now().UTC()
	_, err = s.db.ExecContext(ctx, `UPDATE tunnel_interfaces SET
		mtu = ?, enabled = ?, endpoint_override = ?,
		jc = ?, jmin = ?, jmax = ?, s1 = ?, s2 = ?, h1 = ?, h2 = ?, h3 = ?, h4 = ?,
		i1 = ?, i2 = ?, i3 = ?, i4 = ?, i5 = ?, preset_name = ?, updated_at = ?,
		s3 = ?, s4 = ?, header_protection_key = ?, content_padding_addition = ?,
		rekey_after_time = ?, rekey_timeout = ?, reject_after_time = ?, keepalive_timeout = ?,
		max_handshake_attempts = ?, random_trailers = ?, disable_cookies = ?
		WHERE id = ?`,
		ifc.MTU, boolInt(ifc.Enabled), nullText(ifc.EndpointOverride),
		nullInt(ifc.Obfuscation.Jc, ifc.Obfuscation.Enabled), nullInt(ifc.Obfuscation.Jmin, ifc.Obfuscation.Enabled),
		nullInt(ifc.Obfuscation.Jmax, ifc.Obfuscation.Enabled), nullInt(ifc.Obfuscation.S1, ifc.Obfuscation.Enabled),
		nullInt(ifc.Obfuscation.S2, ifc.Obfuscation.Enabled),
		nullU32(ifc.Obfuscation.H1, ifc.Obfuscation.Enabled), nullU32(ifc.Obfuscation.H2, ifc.Obfuscation.Enabled),
		nullU32(ifc.Obfuscation.H3, ifc.Obfuscation.Enabled), nullU32(ifc.Obfuscation.H4, ifc.Obfuscation.Enabled),
		nullText(ifc.Obfuscation.I1), nullText(ifc.Obfuscation.I2), nullText(ifc.Obfuscation.I3),
		nullText(ifc.Obfuscation.I4), nullText(ifc.Obfuscation.I5),
		ifc.Preset, ifc.UpdatedAt.Format(time.RFC3339Nano),
		ifc.Obfuscation.S3, ifc.Obfuscation.S4,
		ifc.Obfuscation.HeaderProtectionKey, ifc.Obfuscation.ContentPaddingAddition,
		ifc.Obfuscation.RekeyAfterTime, ifc.Obfuscation.RekeyTimeout,
		ifc.Obfuscation.RejectAfterTime, ifc.Obfuscation.KeepaliveTimeout,
		ifc.Obfuscation.MaxHandshakeAttempts,
		boolInt(ifc.Obfuscation.RandomTrailers), boolInt(ifc.Obfuscation.DisableCookies),
		ifc.ID)
	if err != nil {
		return nil, fmt.Errorf("iface: update: %w", err)
	}
	return ifc, nil
}

// Delete removes an interface row. Refused while devices still reference it
// (the caller migrates devices first — part of the rotation workflow).
func (s *Service) Delete(ctx context.Context, id string) error {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM devices WHERE interface_id = ?`, id).Scan(&count); err != nil {
		return fmt.Errorf("iface: device count: %w", err)
	}
	if count > 0 {
		return domain.E(domain.CodeInvalidRequest, "interface still has %d devices; migrate them first (rotation workflow)", count)
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM tunnel_interfaces WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("iface: delete: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.E(domain.CodeInterfaceNotFound, "interface %s not found", id)
	}
	return nil
}

func nullInt(v int, set bool) any {
	if !set {
		return nil
	}
	return v
}

func nullU32(v uint32, set bool) any {
	if !set {
		return nil
	}
	return v
}

func nullText(s string) any {
	if s == "" {
		return nil
	}
	return s
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
