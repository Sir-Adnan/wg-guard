package backup

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/iface"
	"github.com/Sir-Adnan/wg-guard/internal/ipam"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
	"github.com/Sir-Adnan/wg-guard/internal/settings"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel"
)

// All queries inspect the immutable snapshot, never active node/runtime state.
// Expressions are closed source constants selected by the verified schema version.
// Unknown historical settings are preserved; currently supported values are checked.
func inspectDomainData(ctx context.Context, db *sql.DB, version string, cipher *secrets.Cipher) error {
	pools, err := inspectProfiles(ctx, db, version, cipher)
	if err != nil {
		return err
	}
	if err := inspectDevices(ctx, db, pools, cipher); err != nil {
		return err
	}
	if err := inspectEntitlements(ctx, db, version); err != nil {
		return err
	}
	if version >= "0004_sub_links.sql" {
		if err := inspectCustomerLinks(ctx, db, cipher); err != nil {
			return err
		}
	}
	return inspectSettings(ctx, db, cipher)
}

func domainInspectionError(err error) error {
	return verificationError(safetyError("data_semantics", err))
}

// Bound SQL values before SQLite allocates a Go string/slice. NULL signals an
// oversized value; the scan fails with safe guidance. The expression is internal.
func boundedColumn(expression string, limit int) string {
	return "CASE WHEN length(CAST(" + expression + " AS BLOB))<=" + strconv.Itoa(limit) + " THEN " + expression + " END"
}

func inspectProfiles(ctx context.Context, db *sql.DB, version string, cipher *secrets.Cipher) (map[string][]netip.Prefix, error) {
	h := []string{"h1", "h2", "h3", "h4"}
	if version >= "0007_awg_ranges.sql" {
		h = []string{"h1_range", "h2_range", "h3_range", "h4_range"}
	}
	limits := map[string]int{"id": 128, "name": 16, "ipv4_subnet": 32, "public_key": 64, "endpoint_override": 1024}
	for _, field := range h {
		limits[field] = 32
	}
	for _, field := range []string{"i1", "i2", "i3", "i4", "i5"} {
		limits[field] = 8192
	}
	if version >= "0005_iface_advanced.sql" {
		for _, field := range []string{"header_protection_key", "content_padding_addition", "rekey_after_time", "rekey_timeout", "reject_after_time", "keepalive_timeout", "max_handshake_attempts"} {
			limits[field] = 64
		}
	}
	if version >= "0015_interface_ipv4_pools.sql" {
		limits["ipv4_extra_pools"] = 2048
	}
	if err := checkColumnSizes(ctx, db, "tunnel_interfaces", limits); err != nil {
		return nil, err
	}
	fields := []string{
		boundedColumn("id", 128), boundedColumn("name", 16), "listen_port", "mtu", "enabled",
		boundedColumn("ipv4_subnet", 32), boundedColumn("public_key", 64), boundedColumn("private_key_encrypted", secrets.MaxStoredEnvelopeBytes),
		"jc", "jmin", "jmax", "s1", "s2",
	}
	for _, field := range h {
		fields = append(fields, boundedColumn(field, 32))
	}
	for _, field := range []string{"i1", "i2", "i3", "i4", "i5"} {
		fields = append(fields, boundedColumn("COALESCE("+field+",'')", 8192))
	}
	fields = append(fields, boundedColumn("backend_mode", 16), boundedColumn("COALESCE(endpoint_override,'')", 1024))
	advanced := []string{"s3", "s4", "header_protection_key", "content_padding_addition", "rekey_after_time", "rekey_timeout", "reject_after_time", "keepalive_timeout", "max_handshake_attempts", "random_trailers", "disable_cookies"}
	for index, field := range advanced {
		expression := field
		if version < "0005_iface_advanced.sql" {
			expression = "''"
			if index < 2 || index > 8 {
				expression = "0"
			}
		}
		fields = append(fields, boundedColumn(expression, 64))
	}
	extras := "'[]'"
	if version >= "0015_interface_ipv4_pools.sql" {
		extras = "ipv4_extra_pools"
	}
	fields = append(fields, boundedColumn(extras, 2048))
	rows, err := db.QueryContext(ctx, "SELECT "+strings.Join(fields, ",")+" FROM tunnel_interfaces")
	if err != nil {
		return nil, domainInspectionError(err)
	}
	defer rows.Close()
	pools := map[string][]netip.Prefix{}
	var all []netip.Prefix
	for rows.Next() {
		var id, name, subnet, public, mode, endpoint, extra string
		var private []byte
		var port, mtu, enabled, trailers, cookies int
		var jc, jmin, jmax, s1, s2 sql.NullInt64
		var o iface.Obfuscation
		if err := rows.Scan(&id, &name, &port, &mtu, &enabled, &subnet, &public, &private, &jc, &jmin, &jmax, &s1, &s2,
			&o.H1, &o.H2, &o.H3, &o.H4, &o.I1, &o.I2, &o.I3, &o.I4, &o.I5, &mode, &endpoint,
			&o.S3, &o.S4, &o.HeaderProtectionKey, &o.ContentPaddingAddition, &o.RekeyAfterTime, &o.RekeyTimeout,
			&o.RejectAfterTime, &o.KeepaliveTimeout, &o.MaxHandshakeAttempts, &trailers, &cookies, &extra); err != nil {
			return nil, domainInspectionError(err)
		}
		// The name language has only 1110 possible values (1–3 decimal digits).
		if id == "" || !iface.ValidName(name) || len(pools) >= 1110 || !domain.BackendMode(mode).Valid() ||
			port < 1024 || port > 65535 || mtu < 576 || mtu > 65535 || enabled < 0 || enabled > 1 || trailers < 0 || trailers > 1 || cookies < 0 || cookies > 1 {
			return nil, domainInspectionError(nil)
		}
		if _, exists := pools[id]; exists {
			return nil, domainInspectionError(nil)
		}
		o.Enabled, o.Jc, o.Jmin, o.Jmax, o.S1, o.S2 = jc.Valid, int(jc.Int64), int(jmin.Int64), int(jmax.Int64), int(s1.Int64), int(s2.Int64)
		o.RandomTrailers, o.DisableCookies = trailers == 1, cookies == 1
		if iface.ValidateObfuscation(o) != nil || settings.ValidEndpoint(endpoint) != nil || checkStoredKeyPair(cipher, private, public) != nil {
			return nil, domainInspectionError(nil)
		}
		decoded, err := ipam.Decode(subnet, extra)
		if err != nil {
			return nil, domainInspectionError(nil)
		}
		for _, value := range decoded {
			prefix, _ := ipam.Parse(value) // Decode already validates the same contract.
			pools[id] = append(pools[id], prefix)
			all = append(all, prefix)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, domainInspectionError(err)
	}
	// Sorted intervals avoid a quadratic comparison for large stored inventories.
	sort.Slice(all, func(i, j int) bool { return all[i].Addr().Less(all[j].Addr()) })
	for index := 1; index < len(all); index++ {
		if !ipam.Last(all[index-1]).Less(all[index].Addr()) {
			return nil, domainInspectionError(nil)
		}
	}
	return pools, nil
}

func checkStoredKeyPair(cipher *secrets.Cipher, encrypted []byte, public string) error {
	if cipher == nil {
		return domainInspectionError(nil)
	}
	private, err := cipher.Decrypt(encrypted)
	defer clear(private)
	if err != nil {
		return domainInspectionError(nil)
	}
	return tunnel.ValidateKeyPair(string(private), public)
}

func inspectDevices(ctx context.Context, db *sql.DB, pools map[string][]netip.Prefix, cipher *secrets.Cipher) error {
	conditions := []string{"enabled IS NULL", "enabled NOT IN (0,1)"}
	for _, field := range []string{"enabled", "rx_bytes", "tx_bytes", "last_rx", "last_tx"} {
		conditions = append(conditions, field+"<0", "typeof("+field+")<>'integer'")
	}
	var invalid int
	if err := db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM devices WHERE "+strings.Join(conditions, " OR ")+")").Scan(&invalid); err != nil || invalid != 0 {
		return domainInspectionError(err)
	}
	if err := checkColumnSizes(ctx, db, "devices", map[string]int{"interface_id": 128, "ipv4_address": 32, "public_key": 64}); err != nil {
		return err
	}
	query := "SELECT " + strings.Join([]string{boundedColumn("interface_id", 128), boundedColumn("ipv4_address", 32),
		boundedColumn("public_key", 64), boundedColumn("private_key_encrypted", secrets.MaxStoredEnvelopeBytes), boundedColumn("preshared_key_encrypted", secrets.MaxStoredEnvelopeBytes)}, ",") + " FROM devices"
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return domainInspectionError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, address, public string
		var private, psk []byte
		if err := rows.Scan(&id, &address, &public, &private, &psk); err != nil {
			return domainInspectionError(err)
		}
		host, err := netip.ParsePrefix(address)
		if err != nil || !host.Addr().Is4() || host.Bits() != 32 || host.String() != address || checkStoredKeyPair(cipher, private, public) != nil {
			return domainInspectionError(nil)
		}
		assigned := false
		for _, pool := range pools[id] {
			assigned = assigned || ipam.Assignable(pool, host.Addr())
		}
		if !assigned {
			return domainInspectionError(nil)
		}
		if len(psk) > 0 {
			plaintext, err := cipher.Decrypt(psk)
			valid := err == nil && tunnel.ValidateEncodedKey(string(plaintext)) == nil
			clear(plaintext)
			if !valid {
				return domainInspectionError(nil)
			}
		}
	}
	return domainInspectionResult(rows.Err())
}

func inspectEntitlements(ctx context.Context, db *sql.DB, version string) error {
	speeds := []string{"speed_limit_kbps"}
	if version >= "0002_speed_limits.sql" {
		speeds = []string{"speed_limit_down_kbps", "speed_limit_up_kbps"}
	}
	for _, table := range []string{"users", "templates"} {
		conditions := []string{"traffic_limit_bytes<0", "duration_seconds<=0", "device_limit<=0", "enabled IS NULL", "enabled NOT IN (0,1)"}
		numeric := []string{"traffic_limit_bytes", "duration_seconds", "device_limit", "enabled"}
		for _, column := range speeds {
			conditions = append(conditions, column+"<=0")
			numeric = append(numeric, column)
		}
		if table == "users" {
			conditions = append(conditions, "traffic_used_rx<0", "traffic_used_tx<0", "traffic_used_rx IS NULL", "traffic_used_tx IS NULL", "traffic_used_rx > 9223372036854775807 - traffic_used_tx")
			numeric = append(numeric, "traffic_used_rx", "traffic_used_tx")
		}
		for _, column := range numeric {
			conditions = append(conditions, "("+column+" IS NOT NULL AND typeof("+column+")<>'integer')")
		}
		var invalid int
		err := db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM "+table+" WHERE "+strings.Join(conditions, " OR ")+")").Scan(&invalid)
		if err != nil || invalid != 0 {
			return domainInspectionError(err)
		}
		if err := inspectLifecycle(ctx, db, table); err != nil {
			return err
		}
	}
	type dateFields struct {
		table    string
		dates    []string
		required int
	}
	dates := []dateFields{
		{"tunnel_interfaces", []string{"created_at", "updated_at"}, 2},
		{"users", []string{"created_at", "updated_at", "activated_at", "expires_at", "last_activity_at", "deleted_at"}, 2},
		{"templates", []string{"created_at", "updated_at"}, 2},
		{"devices", []string{"created_at", "updated_at", "last_handshake_at"}, 2},
		{"admins", []string{"created_at", "updated_at"}, 2},
		{"admin_sessions", []string{"created_at", "last_seen_at", "expires_at"}, 3},
		{"api_tokens", []string{"created_at", "expires_at", "last_used_at"}, 1},
	}
	if version >= "0004_sub_links.sql" {
		dates = append(dates, dateFields{"sub_links", []string{"created_at", "revoked_at", "rotated_at"}, 1})
	}
	for _, entry := range dates {
		fields := make([]string, len(entry.dates))
		limits := map[string]int{}
		for index, date := range entry.dates {
			fields[index] = boundedColumn(date, 64)
			limits[date] = 64
		}
		if err := checkColumnSizes(ctx, db, entry.table, limits); err != nil {
			return err
		}
		rows, err := db.QueryContext(ctx, "SELECT "+strings.Join(fields, ",")+" FROM "+entry.table)
		if err != nil {
			return domainInspectionError(err)
		}
		values := make([]sql.NullString, len(fields))
		dest := make([]any, len(fields))
		for index := range values {
			dest[index] = &values[index]
		}
		for rows.Next() {
			if err := rows.Scan(dest...); err != nil {
				rows.Close()
				return domainInspectionError(err)
			}
			for index, value := range values {
				if !value.Valid && index >= entry.required {
					continue
				}
				if _, err := time.Parse(time.RFC3339Nano, value.String); err != nil {
					rows.Close()
					return domainInspectionError(nil)
				}
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return domainInspectionError(err)
		}
	}
	return nil
}

func inspectLifecycle(ctx context.Context, db *sql.DB, table string) error {
	columns := []string{boundedColumn("start_policy", 32), "NULL", "NULL"}
	limits := map[string]int{"start_policy": 32}
	if table == "users" {
		columns[1], columns[2] = boundedColumn("status", 32), boundedColumn("disable_reason", 32)
		limits["status"], limits["disable_reason"] = 32, 32
	}
	if err := checkColumnSizes(ctx, db, table, limits); err != nil {
		return err
	}
	rows, err := db.QueryContext(ctx, "SELECT "+strings.Join(columns, ",")+" FROM "+table)
	if err != nil {
		return domainInspectionError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var policy, status, reason sql.NullString
		if err := rows.Scan(&policy, &status, &reason); err != nil {
			return domainInspectionError(err)
		}
		if !policy.Valid || !domain.StartPolicy(policy.String).Valid() {
			return domainInspectionError(nil)
		}
		if table == "users" && (!status.Valid || !domain.UserStatus(status.String).Valid() || (reason.Valid && !domain.DisableReason(reason.String).Valid())) {
			return domainInspectionError(nil)
		}
	}
	return domainInspectionResult(rows.Err())
}

func inspectCustomerLinks(ctx context.Context, db *sql.DB, cipher *secrets.Cipher) error {
	if err := checkColumnSizes(ctx, db, "sub_links", map[string]int{"token_hash": 64}); err != nil {
		return err
	}
	rows, err := db.QueryContext(ctx, "SELECT "+boundedColumn("token_encrypted", secrets.MaxStoredEnvelopeBytes)+","+boundedColumn("token_hash", 64)+" FROM sub_links")
	if err != nil {
		return domainInspectionError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var envelope []byte
		var hash string
		if err := rows.Scan(&envelope, &hash); err != nil {
			return domainInspectionError(err)
		}
		if cipher == nil {
			return domainInspectionError(nil)
		}
		plaintext, err := cipher.Decrypt(envelope)
		decoded, decodeErr := base64.RawURLEncoding.DecodeString(string(plaintext))
		sum := sha256.Sum256(plaintext)
		valid := err == nil && decodeErr == nil && len(decoded) == 32 && base64.RawURLEncoding.EncodeToString(decoded) == string(plaintext) && hex.EncodeToString(sum[:]) == hash
		clear(decoded)
		clear(plaintext)
		if !valid {
			return domainInspectionError(nil)
		}
	}
	return domainInspectionResult(rows.Err())
}

func inspectSettings(ctx context.Context, db *sql.DB, cipher *secrets.Cipher) error {
	definitions := map[string]settings.Definition{}
	for _, definition := range settings.Defaults() {
		definitions[definition.Key] = definition
	}
	if err := checkColumnSizes(ctx, db, "settings", map[string]int{"key": 128, "value": 1 << 20}); err != nil {
		return err
	}
	rows, err := db.QueryContext(ctx, "SELECT "+boundedColumn("key", 128)+","+boundedColumn("value", 1<<20)+" FROM settings")
	if err != nil {
		return domainInspectionError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return domainInspectionError(err)
		}
		definition, known := definitions[key]
		if !known {
			continue
		}
		if definition.Secret {
			if cipher == nil {
				return domainInspectionError(nil)
			}
			if !validSecretSetting(cipher, definition, value) {
				return domainInspectionError(nil)
			}
		} else if settings.ValidateStoredValue(definition, value) != nil {
			return domainInspectionError(nil)
		}
	}
	return domainInspectionResult(rows.Err())
}

func validSecretSetting(cipher *secrets.Cipher, definition settings.Definition, value string) bool {
	envelope, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, "enc:"))
	defer clear(envelope)
	if err != nil || !strings.HasPrefix(value, "enc:") {
		return false
	}
	plaintext, err := cipher.Decrypt(envelope)
	defer clear(plaintext)
	return err == nil && settings.ValidateStoredValue(definition, string(plaintext)) == nil
}

func checkColumnSizes(ctx context.Context, db *sql.DB, table string, limits map[string]int) error {
	conditions := make([]string, 0, len(limits))
	for column, limit := range limits {
		conditions = append(conditions, "length(CAST("+column+" AS BLOB))>"+strconv.Itoa(limit))
	}
	var oversized int
	if err := db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM "+table+" WHERE "+strings.Join(conditions, " OR ")+")").Scan(&oversized); err != nil {
		return domainInspectionError(err)
	}
	if oversized != 0 {
		return domainInspectionError(nil)
	}
	return nil
}

func domainInspectionResult(err error) error {
	if err != nil {
		return domainInspectionError(err)
	}
	return nil
}
