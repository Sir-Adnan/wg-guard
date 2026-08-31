// Package device implements the peer/device domain service. The two
// invariants the product can never afford to break are enforced here inside
// BEGIN IMMEDIATE transactions (database.md): the per-user device limit and
// the unique per-interface IPv4 allocation. Keys arrive encrypted by the
// caller (secrets.KeyRing); this package never sees plaintext private keys
// in logs.
package device

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel"
)

// Device is a stored peer.
type Device struct {
	ID            string
	UserID        string
	InterfaceID   string
	Name          string
	IPv4          string // with /32
	PublicKey     string
	PrivKeyEnc    []byte
	PSKEnc        []byte // nil = no preshared key
	Enabled       bool
	LastHandshake *time.Time
	LastEndpoint  string
	RXBytes       uint64
	TXBytes       uint64
	LastRX        uint64
	LastTX        uint64
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// KeyMaterial is the caller-provided keypair for a new device.
type KeyMaterial struct {
	PublicKey     string
	PrivateKeyEnc []byte // AES-GCM envelope from secrets.KeyRing
	PresharedEnc  []byte // optional; nil = none
}

// Service holds device rules.
type Service struct {
	db   *database.DB
	ring *secrets.KeyRing
	now  func() time.Time

	// Recorder (optional, satisfied by *webhook.Recorder) emits durable
	// device events inside the state-changing transaction (webhooks.md).
	Recorder interface {
		RecordTx(tx *sql.Tx, eventType string, data map[string]any) error
	}
}

func NewService(db *database.DB, ring *secrets.KeyRing) *Service {
	return &Service{db: db, ring: ring, now: time.Now}
}

// Create provisions a device for a user inside one immediate transaction:
// limit check → free-IP allocation → insert. Concurrent creates serialize on
// the write lock, so the limit can never be exceeded by a race (tested).
func (s *Service) Create(ctx context.Context, userID string, name string, keys KeyMaterial, interfaceID string) (*Device, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 64 {
		return nil, domain.E(domain.CodeInvalidRequest, "device name must be 1-64 characters")
	}
	if err := tunnel.ValidatePublicKey(keys.PublicKey); err != nil {
		return nil, domain.E(domain.CodeInvalidRequest, "%v", err)
	}
	if len(keys.PrivateKeyEnc) == 0 {
		return nil, domain.E(domain.CodeInvalidRequest, "encrypted private key required")
	}

	d := &Device{
		ID:         domain.NewID(),
		UserID:     userID,
		Name:       name,
		PublicKey:  keys.PublicKey,
		PrivKeyEnc: keys.PrivateKeyEnc,
		PSKEnc:     keys.PresharedEnc,
		Enabled:    true,
		CreatedAt:  s.now().UTC(),
		UpdatedAt:  s.now().UTC(),
	}

	err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
		// User must exist, be live, and be peer-eligible.
		var status string
		var enabled int
		err := tx.QueryRowContext(ctx, `SELECT status, enabled FROM users WHERE id = ? AND deleted_at IS NULL`, userID).
			Scan(&status, &enabled)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.E(domain.CodeUserNotFound, "user %s not found", userID)
		}
		if err != nil {
			return fmt.Errorf("device: user lookup: %w", err)
		}
		if enabled == 0 {
			return domain.E(domain.CodeForbidden, "user %s is disabled", userID)
		}
		if st := domain.UserStatus(status); !st.PeerWanted() {
			return domain.E(domain.CodeForbidden, "user %s is %s; devices cannot be added", userID, status)
		}

		// Interface: explicit, else the user's profile, else the first
		// enabled interface (name order).
		ifcID := interfaceID
		if ifcID == "" {
			err := tx.QueryRowContext(ctx, `
				SELECT COALESCE(
					(SELECT interface_id FROM users WHERE id = ?),
					(SELECT id FROM tunnel_interfaces WHERE enabled = 1 ORDER BY name LIMIT 1)
				)`, userID).Scan(&ifcID)
			if errors.Is(err, sql.ErrNoRows) || ifcID == "" {
				return domain.E(domain.CodeInterfaceNotFound, "no tunnel interface available; create a profile first")
			}
			if err != nil {
				return fmt.Errorf("device: interface lookup: %w", err)
			}
		}
		var subnet string
		err = tx.QueryRowContext(ctx, `SELECT ipv4_subnet FROM tunnel_interfaces WHERE id = ? AND enabled = 1`, ifcID).
			Scan(&subnet)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.E(domain.CodeInterfaceNotFound, "interface %s not found or disabled", ifcID)
		}
		if err != nil {
			return fmt.Errorf("device: interface lookup: %w", err)
		}
		d.InterfaceID = ifcID

		// Device limit: count + insert in the same serialized transaction.
		var limit *int
		var userLimit sql.NullInt64
		if err := tx.QueryRowContext(ctx, `SELECT device_limit FROM users WHERE id = ?`, userID).Scan(&userLimit); err != nil {
			return fmt.Errorf("device: limit lookup: %w", err)
		}
		if userLimit.Valid {
			v := int(userLimit.Int64)
			limit = &v
		}
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM devices WHERE user_id = ?`, userID).Scan(&count); err != nil {
			return fmt.Errorf("device: count: %w", err)
		}
		if limit != nil && count >= *limit {
			return domain.E(domain.CodeDeviceLimitReached,
				"user %s already has %d devices (limit %d)", userID, count, *limit)
		}

		// Duplicate key guard (explicit code beats a bare UNIQUE error).
		var pk string
		if err := tx.QueryRowContext(ctx, `SELECT public_key FROM devices WHERE public_key = ?`, keys.PublicKey).Scan(&pk); err == nil {
			return domain.E(domain.CodeDeviceKeyExists, "this public key is already registered")
		} else if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("device: key check: %w", err)
		}

		// IPv4 allocation: first free address in the pool (network+1 …
		// broadcast-1). The UNIQUE(interface_id, ipv4) constraint is the
		// backstop; serialization makes the scan trustworthy.
		ip, err := allocateIP(ctx, tx, ifcID, subnet)
		if err != nil {
			return err
		}
		d.IPv4 = ip

		// Device name unique per user (explicit check for a clean code).
		var dn string
		if err := tx.QueryRowContext(ctx, `SELECT name FROM devices WHERE user_id = ? AND name = ?`, userID, name).Scan(&dn); err == nil {
			return domain.E(domain.CodeInvalidRequest, "device name %q already exists for this user", name)
		} else if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("device: name check: %w", err)
		}

		if err := insertDevice(ctx, tx, d); err != nil {
			return err
		}
		if s.Recorder != nil {
			return s.Recorder.RecordTx(tx, "device.created", map[string]any{
				"device_id": d.ID, "user_id": d.UserID, "name": d.Name, "ipv4": d.IPv4,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return d, nil
}

func allocateIP(ctx context.Context, tx *sql.Tx, interfaceID, subnet string) (string, error) {
	prefix, err := netip.ParsePrefix(subnet)
	if err != nil {
		return "", domain.E(domain.CodeSubnetInvalid, "interface pool %q is invalid", subnet)
	}
	rows, err := tx.QueryContext(ctx, `SELECT ipv4_address FROM devices WHERE interface_id = ?`, interfaceID)
	if err != nil {
		return "", fmt.Errorf("device: ip scan: %w", err)
	}
	used := map[string]bool{}
	for rows.Next() {
		var ip string
		if err := rows.Scan(&ip); err != nil {
			rows.Close()
			return "", fmt.Errorf("device: ip scan: %w", err)
		}
		if p, err := netip.ParsePrefix(ip); err == nil {
			used[p.Addr().String()] = true
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("device: ip scan: %w", err)
	}

	// Iterate candidate host addresses. The first host address is reserved
	// for the interface itself (gateway convention, .1 of the pool); device
	// addresses start at .2. Pools are ≥ /29 by validation and typically
	// /24 → at most a few hundred probes; fine for SQLite.
	masked := prefix.Masked()
	gateway := masked.Addr().Next()
	bcast := broadcastOf(prefix)
	for a := gateway; prefix.Contains(a); a = a.Next() {
		if a == gateway || a == bcast {
			continue
		}
		if !used[a.String()] {
			return a.String() + "/32", nil
		}
	}
	return "", domain.E(domain.CodeDevicePoolExhausted, "interface pool %s has no free addresses", subnet)
}

func broadcastOf(p netip.Prefix) netip.Addr {
	// IPv4 only (validated upstream): compute the last address in the range.
	raw := p.Masked().Addr().As4()
	hostBits := 32 - p.Bits()
	var hostMask uint32 = ^(uint32(1)<<hostBits - 1)
	base := uint32(raw[0])<<24 | uint32(raw[1])<<16 | uint32(raw[2])<<8 | uint32(raw[3])
	bcast := base | ^hostMask
	return netip.AddrFrom4([4]byte{byte(bcast >> 24), byte(bcast >> 16), byte(bcast >> 8), byte(bcast)})
}

func insertDevice(ctx context.Context, tx *sql.Tx, d *Device) error {
	var psk any
	if d.PSKEnc != nil {
		psk = d.PSKEnc
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO devices
		(id, user_id, interface_id, name, ipv4_address, public_key,
		 private_key_encrypted, preshared_key_encrypted, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.ID, d.UserID, d.InterfaceID, d.Name, d.IPv4, d.PublicKey,
		d.PrivKeyEnc, psk, boolInt(d.Enabled),
		d.CreatedAt.Format(time.RFC3339Nano), d.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			if strings.Contains(err.Error(), "ipv4_address") {
				return domain.E(domain.CodeDevicePoolExhausted, "address allocation conflict")
			}
			return domain.E(domain.CodeDeviceKeyExists, "device key or name already exists")
		}
		return fmt.Errorf("device: insert: %w", err)
	}
	return nil
}

// Get loads a device by ID.
func (s *Service) Get(ctx context.Context, id string) (*Device, error) {
	row := s.db.QueryRowContext(ctx, deviceColumns+` FROM devices WHERE id = ?`, id)
	d, err := scanDevice(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.E(domain.CodeDeviceNotFound, "device %s not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("device: get: %w", err)
	}
	return d, nil
}

// ListForUser returns the user's devices, newest first.
func (s *Service) ListForUser(ctx context.Context, userID string) ([]*Device, error) {
	rows, err := s.db.QueryContext(ctx, deviceColumns+` FROM devices WHERE user_id = ? ORDER BY created_at DESC, id DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("device: list: %w", err)
	}
	defer rows.Close()
	var out []*Device
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, fmt.Errorf("device: scan: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ListForUsers batch-loads the devices of a page of users (one query per
// page for the list-view quick-share menus), newest first within each user.
func (s *Service) ListForUsers(ctx context.Context, userIDs []string) (map[string][]*Device, error) {
	out := make(map[string][]*Device, len(userIDs))
	if len(userIDs) == 0 {
		return out, nil
	}
	placeholders := strings.Repeat("?,", len(userIDs))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, len(userIDs))
	for i, id := range userIDs {
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx,
		deviceColumns+` FROM devices WHERE user_id IN (`+placeholders+`) ORDER BY created_at DESC, id DESC`,
		args...)
	if err != nil {
		return nil, fmt.Errorf("device: list batch: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, fmt.Errorf("device: list batch scan: %w", err)
		}
		out[d.UserID] = append(out[d.UserID], d)
	}
	return out, rows.Err()
}

// CountForUser returns the live device count.
func (s *Service) CountForUser(ctx context.Context, userID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM devices WHERE user_id = ?`, userID).Scan(&n)
	return n, err
}

// CountForUsers returns device counts for a batch of users (one query per
// page, not per row). Missing IDs count 0.
func (s *Service) CountForUsers(ctx context.Context, userIDs []string) (map[string]int, error) {
	counts := make(map[string]int, len(userIDs))
	if len(userIDs) == 0 {
		return counts, nil
	}
	placeholders := strings.Repeat("?,", len(userIDs))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, len(userIDs))
	for i, id := range userIDs {
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT user_id, COUNT(*) FROM devices WHERE user_id IN (`+placeholders+`) GROUP BY user_id`,
		args...)
	if err != nil {
		return nil, fmt.Errorf("device: count batch: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, fmt.Errorf("device: count batch scan: %w", err)
		}
		counts[id] = n
	}
	return counts, rows.Err()
}

// CountForIfaces returns device counts per interface id (batch for the
// interface list). Counts all devices — including those of soft-deleted
// users — to mirror the interface Delete guard.
func (s *Service) CountForIfaces(ctx context.Context, ifaceIDs []string) (map[string]int, error) {
	counts := make(map[string]int, len(ifaceIDs))
	if len(ifaceIDs) == 0 {
		return counts, nil
	}
	placeholders := strings.Repeat("?,", len(ifaceIDs))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, len(ifaceIDs))
	for i, id := range ifaceIDs {
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT interface_id, COUNT(*) FROM devices WHERE interface_id IN (`+placeholders+`) GROUP BY interface_id`,
		args...)
	if err != nil {
		return nil, fmt.Errorf("device: count by iface: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, fmt.Errorf("device: count by iface scan: %w", err)
		}
		counts[id] = n
	}
	return counts, rows.Err()
}

// SetEnabled toggles a device (disabled devices are removed from the backend
// by reconciliation).
func (s *Service) SetEnabled(ctx context.Context, id string, enabled bool) error {
	res, err := s.db.ExecContext(ctx, `UPDATE devices SET enabled = ?, updated_at = ? WHERE id = ?`,
		boolInt(enabled), s.now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("device: set enabled: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.E(domain.CodeDeviceNotFound, "device %s not found", id)
	}
	return nil
}

// Rename changes the device display name (unique per user).
func (s *Service) Rename(ctx context.Context, id, name string) (*Device, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 64 {
		return nil, domain.E(domain.CodeInvalidRequest, "device name must be 1-64 characters")
	}
	d, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	var existing string
	err = s.db.QueryRowContext(ctx, `SELECT name FROM devices WHERE user_id = ? AND name = ? AND id != ?`,
		d.UserID, name, id).Scan(&existing)
	if err == nil {
		return nil, domain.E(domain.CodeInvalidRequest, "device name %q already exists for this user", name)
	} else if err != sql.ErrNoRows {
		return nil, fmt.Errorf("device: rename check: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE devices SET name = ?, updated_at = ? WHERE id = ?`,
		name, s.now().UTC().Format(time.RFC3339Nano), id); err != nil {
		return nil, fmt.Errorf("device: rename: %w", err)
	}
	d.Name = name
	d.UpdatedAt = s.now().UTC()
	return d, nil
}

// Regenerate replaces the device keys (caller supplies the new encrypted
// keypair; the public key change propagates to the backend via reconcile).
func (s *Service) Regenerate(ctx context.Context, id string, keys KeyMaterial) error {
	if err := tunnel.ValidatePublicKey(keys.PublicKey); err != nil {
		return domain.E(domain.CodeInvalidRequest, "%v", err)
	}
	if len(keys.PrivateKeyEnc) == 0 {
		return domain.E(domain.CodeInvalidRequest, "encrypted private key required")
	}
	var psk any
	if keys.PresharedEnc != nil {
		psk = keys.PresharedEnc
	}
	res, err := s.db.ExecContext(ctx, `UPDATE devices SET public_key = ?, private_key_encrypted = ?,
		preshared_key_encrypted = ?, updated_at = ? WHERE id = ?`,
		keys.PublicKey, keys.PrivateKeyEnc, psk, s.now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return domain.E(domain.CodeDeviceKeyExists, "key already registered")
		}
		return fmt.Errorf("device: regenerate: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.E(domain.CodeDeviceNotFound, "device %s not found", id)
	}
	return nil
}

// Delete permanently removes a device and releases its VPN IP.
func (s *Service) Delete(ctx context.Context, id string) error {
	return s.db.WithTx(ctx, func(tx *sql.Tx) error {
		var (
			userID, name, ipv4 string
		)
		err := tx.QueryRowContext(ctx, `SELECT user_id, name, ipv4_address FROM devices WHERE id = ?`, id).
			Scan(&userID, &name, &ipv4)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.E(domain.CodeDeviceNotFound, "device %s not found", id)
		}
		if err != nil {
			return fmt.Errorf("device: delete lookup: %w", err)
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM devices WHERE id = ?`, id)
		if err != nil {
			return fmt.Errorf("device: delete: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return domain.E(domain.CodeDeviceNotFound, "device %s not found", id)
		}
		if s.Recorder != nil {
			return s.Recorder.RecordTx(tx, "device.deleted", map[string]any{
				"device_id": id, "user_id": userID, "name": name, "ipv4": ipv4,
			})
		}
		return nil
	})
}

// PrivateKey decrypts the stored private key (config generation and peer
// apply only — never logged, never serialized to API responses).
func (s *Service) PrivateKey(ctx context.Context, d *Device) (string, error) {
	pt, err := s.ring.Decrypt(d.PrivKeyEnc)
	if err != nil {
		return "", fmt.Errorf("device: decrypt private key: %w", err)
	}
	return string(pt), nil
}

// PresharedKey decrypts the stored preshared key ("" when none).
func (s *Service) PresharedKey(ctx context.Context, d *Device) (string, error) {
	if d.PSKEnc == nil {
		return "", nil
	}
	pt, err := s.ring.Decrypt(d.PSKEnc)
	if err != nil {
		return "", fmt.Errorf("device: decrypt psk: %w", err)
	}
	return string(pt), nil
}

// ReencryptSecrets rotates device key envelopes (master-key rotation
// carrier). Runs outside a transaction deliberately: rotation order in
// secrets.Rotate guarantees both keys stay available (see package docs).
func (s *Service) ReencryptSecrets(from, to *secrets.Cipher) error {
	rows, err := s.db.Query(`SELECT id, private_key_encrypted, preshared_key_encrypted FROM devices`)
	if err != nil {
		return fmt.Errorf("device: rotate scan: %w", err)
	}
	type row struct {
		id  string
		pk  []byte
		psk []byte
	}
	var updates []row
	for rows.Next() {
		var r row
		var psk []byte
		if err := rows.Scan(&r.id, &r.pk, &psk); err != nil {
			rows.Close()
			return fmt.Errorf("device: rotate: %w", err)
		}
		pt, err := from.Decrypt(r.pk)
		if err != nil {
			rows.Close()
			return fmt.Errorf("device: rotate %s: %w", r.id, err)
		}
		r.pk, err = to.Encrypt(pt)
		if err != nil {
			rows.Close()
			return fmt.Errorf("device: rotate %s: %w", r.id, err)
		}
		if psk != nil {
			pt, err := from.Decrypt(psk)
			if err != nil {
				rows.Close()
				return fmt.Errorf("device: rotate %s psk: %w", r.id, err)
			}
			r.psk, err = to.Encrypt(pt)
			if err != nil {
				rows.Close()
				return fmt.Errorf("device: rotate %s psk: %w", r.id, err)
			}
		}
		updates = append(updates, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("device: rotate: %w", err)
	}
	rows.Close()
	for _, r := range updates {
		var psk any
		if r.psk != nil {
			psk = r.psk
		}
		if _, err := s.db.Exec(`UPDATE devices SET private_key_encrypted = ?, preshared_key_encrypted = ? WHERE id = ?`,
			r.pk, psk, r.id); err != nil {
			return fmt.Errorf("device: rotate write %s: %w", r.id, err)
		}
	}
	return nil
}

const deviceColumns = `SELECT id, user_id, interface_id, name, ipv4_address, public_key,
	private_key_encrypted, preshared_key_encrypted, enabled, last_handshake_at, last_endpoint,
	rx_bytes, tx_bytes, last_rx, last_tx, created_at, updated_at`

func scanDevice(row rowScanner) (*Device, error) {
	var (
		d          Device
		psk        []byte
		enabled    int
		handshake  sql.NullString
		endpoint   sql.NullString
		createdStr string
		updatedStr string
	)
	if err := row.Scan(&d.ID, &d.UserID, &d.InterfaceID, &d.Name, &d.IPv4, &d.PublicKey,
		&d.PrivKeyEnc, &psk, &enabled, &handshake, &endpoint,
		&d.RXBytes, &d.TXBytes, &d.LastRX, &d.LastTX, &createdStr, &updatedStr); err != nil {
		return nil, err
	}
	if psk != nil {
		d.PSKEnc = psk
	}
	d.Enabled = enabled == 1
	if handshake.Valid {
		if t, err := time.Parse(time.RFC3339Nano, handshake.String); err == nil {
			d.LastHandshake = &t
		}
	}
	d.LastEndpoint = endpoint.String
	d.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdStr)
	d.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedStr)
	return &d, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

type rowScanner interface{ Scan(dest ...any) error }
