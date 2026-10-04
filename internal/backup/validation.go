package backup

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"os"
	"path/filepath"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/secrets"
)

// Inventory describes stored records, including disabled and legacy deleted
// accounts. It contains no credentials, private keys or customer capabilities.
type Inventory struct {
	Users, Devices, Templates, Interfaces uint64
	Admins, APITokens, CustomerLinks      uint64
	Webhooks, Resellers                   uint64
	KernelInterfaces, UserspaceInterfaces uint64
	EncryptedValues                       uint64
}

const maxSecretEnvelopeBytes = 8 << 10

// inspectArchiveData checks an offline snapshot against the one key included in
// the archive. A previous rotation key on the source host cannot make a portable
// archive valid. Rows stream one bounded envelope at a time; plaintext is cleared
// immediately, and SQL errors/values never become public diagnostic text.
func inspectArchiveData(ctx context.Context, path string, key []byte) (Inventory, error) {
	var inventory Inventory
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	stat, err := os.Lstat(path)
	if err != nil || !stat.Mode().IsRegular() || stat.Size() > maxDatabaseBytes {
		return inventory, safetyError("data_snapshot", err)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro&immutable=1")
	if err != nil {
		return inventory, safetyError("data_inspection", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	var version string
	if err := db.QueryRowContext(ctx, `SELECT MAX(version) FROM migrations`).Scan(&version); err != nil || version == "" {
		return inventory, safetyError("data_inspection", err)
	}
	violations, err := db.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return inventory, safetyError("data_inspection", err)
	}
	broken := violations.Next()
	err = violations.Err()
	violations.Close()
	if err != nil {
		return inventory, safetyError("data_inspection", err)
	}
	if broken {
		return inventory, safetyError("data_references", nil)
	}
	counts := []struct {
		query, since string
		target       *uint64
	}{
		{`SELECT COUNT(*) FROM users`, "", &inventory.Users},
		{`SELECT COUNT(*) FROM devices`, "", &inventory.Devices},
		{`SELECT COUNT(*) FROM templates`, "", &inventory.Templates},
		{`SELECT COUNT(*) FROM tunnel_interfaces`, "", &inventory.Interfaces},
		{`SELECT COUNT(*) FROM admins`, "", &inventory.Admins},
		{`SELECT COUNT(*) FROM api_tokens`, "", &inventory.APITokens},
		{`SELECT COUNT(*) FROM webhook_endpoints`, "", &inventory.Webhooks},
		{`SELECT COUNT(*) FROM sub_links`, "0004_sub_links.sql", &inventory.CustomerLinks},
		{`SELECT COUNT(*) FROM resellers`, "0010_reseller_ownership.sql", &inventory.Resellers},
		{`SELECT COUNT(*) FROM tunnel_interfaces WHERE backend_mode='kernel'`, "", &inventory.KernelInterfaces},
		{`SELECT COUNT(*) FROM tunnel_interfaces WHERE backend_mode='userspace'`, "", &inventory.UserspaceInterfaces},
	}
	for _, count := range counts {
		if version < count.since {
			continue
		}
		if err := db.QueryRowContext(ctx, count.query).Scan(count.target); err != nil {
			return inventory, safetyError("data_inspection", err)
		}
	}
	if inventory.KernelInterfaces+inventory.UserspaceInterfaces != inventory.Interfaces {
		return inventory, safetyError("data_inspection", nil)
	}
	var cipher *secrets.Cipher
	if len(key) != 0 {
		cipher, err = secrets.NewCipher(key)
		if err != nil {
			return inventory, safetyError("data_key", err)
		}
	}
	carriers := []struct {
		query, since string
		text, empty  bool
	}{
		{`SELECT length(CAST(private_key_encrypted AS BLOB)), CASE WHEN length(CAST(private_key_encrypted AS BLOB))<=? THEN CAST(private_key_encrypted AS BLOB) END FROM tunnel_interfaces`, "", false, false},
		{`SELECT length(CAST(private_key_encrypted AS BLOB)), CASE WHEN length(CAST(private_key_encrypted AS BLOB))<=? THEN CAST(private_key_encrypted AS BLOB) END FROM devices`, "", false, false},
		{`SELECT length(CAST(preshared_key_encrypted AS BLOB)), CASE WHEN length(CAST(preshared_key_encrypted AS BLOB))<=? THEN CAST(preshared_key_encrypted AS BLOB) END FROM devices`, "", false, true},
		{`SELECT length(CAST(token_encrypted AS BLOB)), CASE WHEN length(CAST(token_encrypted AS BLOB))<=? THEN CAST(token_encrypted AS BLOB) END FROM sub_links`, "0004_sub_links.sql", false, false},
		{`SELECT length(CAST(secret_encrypted AS BLOB)), CASE WHEN length(CAST(secret_encrypted AS BLOB))<=? THEN CAST(secret_encrypted AS BLOB) END FROM webhook_endpoints`, "", true, false},
		{`SELECT length(CAST(value AS BLOB)), CASE WHEN length(CAST(value AS BLOB))<=? THEN CAST(value AS BLOB) END FROM settings WHERE key IN ('backup.password', 'backup.telegram_token')`, "", true, false},
	}
	for _, carrier := range carriers {
		if version < carrier.since {
			continue
		}
		rows, err := db.QueryContext(ctx, carrier.query, maxSecretEnvelopeBytes)
		if err != nil {
			return inventory, safetyError("data_inspection", err)
		}
		for rows.Next() {
			var size sql.NullInt64
			var envelope []byte
			if err := rows.Scan(&size, &envelope); err != nil {
				rows.Close()
				return inventory, safetyError("data_inspection", err)
			}
			if carrier.empty && (!size.Valid || size.Int64 == 0) {
				continue
			}
			if !size.Valid || size.Int64 <= 0 || size.Int64 > maxSecretEnvelopeBytes {
				rows.Close()
				return inventory, safetyError("data_envelope", nil)
			}
			if cipher == nil {
				rows.Close()
				return inventory, safetyError("data_key", nil)
			}
			if carrier.text {
				if !bytes.HasPrefix(envelope, []byte("enc:")) {
					rows.Close()
					return inventory, safetyError("data_envelope", nil)
				}
				decoded := make([]byte, base64.StdEncoding.DecodedLen(len(envelope)-4))
				n, err := base64.StdEncoding.Decode(decoded, envelope[4:])
				clear(envelope)
				if err != nil {
					clear(decoded)
					rows.Close()
					return inventory, safetyError("data_envelope", nil)
				}
				envelope = decoded[:n]
			}
			plaintext, err := cipher.Decrypt(envelope)
			clear(plaintext)
			clear(envelope)
			if err != nil {
				rows.Close()
				return inventory, safetyError("data_key", nil)
			}
			inventory.EncryptedValues++
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return inventory, safetyError("data_inspection", err)
		}
	}
	return inventory, nil
}

func inspectStagedData(ctx context.Context, dir string) (Inventory, error) {
	key, err := readSmall(filepath.Join(dir, KeyMember), 32)
	if err != nil && !os.IsNotExist(err) {
		return Inventory{}, safetyError("data_key", err)
	}
	defer clear(key)
	return inspectArchiveData(ctx, filepath.Join(dir, DBMember), key)
}
