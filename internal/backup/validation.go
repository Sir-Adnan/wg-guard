package backup

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
)

// Inventory describes stored records, including disabled and legacy deleted
// accounts. It contains no credentials, private keys or customer capabilities.
type Inventory struct {
	Users, Devices, Templates, Interfaces uint64
	Admins, APITokens, CustomerLinks      uint64
	EnabledOwners                         uint64
	Webhooks, Resellers                   uint64
	KernelInterfaces, UserspaceInterfaces uint64
	EncryptedValues                       uint64
}

// inspectArchiveData checks an offline snapshot against the one key included in
// the archive. A previous rotation key on the source host cannot make a portable
// archive valid. Rows stream one bounded envelope at a time; plaintext is cleared
// immediately, and SQL errors/values never become public diagnostic text.
func inspectArchiveData(ctx context.Context, path string, key []byte) (Inventory, error) {
	var inventory Inventory
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return inventory, verificationError(err)
	}
	stat, err := os.Lstat(path)
	if err != nil || !stat.Mode().IsRegular() || stat.Size() > maxDatabaseBytes {
		return inventory, safetyError("data_snapshot", err)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro&immutable=1")
	if err != nil {
		return inventory, verificationError(safetyError("data_inspection", err))
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if status, err := (&database.DB{DB: db}).MigrationStatus(ctx); err != nil || status.Applied == 0 {
		return inventory, verificationError(safetyError("data_inspection", err))
	}
	var version string
	if err := db.QueryRowContext(ctx, `SELECT MAX(version) FROM migrations`).Scan(&version); err != nil || version == "" {
		return inventory, verificationError(safetyError("data_inspection", err))
	}
	violations, err := db.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return inventory, verificationError(safetyError("data_inspection", err))
	}
	broken := violations.Next()
	err = violations.Err()
	violations.Close()
	if err != nil {
		return inventory, verificationError(safetyError("data_inspection", err))
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
			return inventory, verificationError(safetyError("data_inspection", err))
		}
	}
	ownerQuery := `SELECT COUNT(*) FROM admins WHERE role='owner' AND enabled=1`
	if version >= "0010_reseller_ownership.sql" {
		ownerQuery += ` AND reseller_id IS NULL`
	}
	if err := db.QueryRowContext(ctx, ownerQuery).Scan(&inventory.EnabledOwners); err != nil {
		return inventory, verificationError(safetyError("data_inspection", err))
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
	err = secrets.WalkStoredSecrets(ctx, db, version, false, func(envelope []byte) error {
		if cipher == nil {
			return safetyError("data_key", nil)
		}
		plaintext, err := cipher.Decrypt(envelope)
		clear(plaintext)
		if err != nil {
			return safetyError("data_key", nil)
		}
		inventory.EncryptedValues++
		return nil
	})
	if err != nil {
		var message Message
		if errors.As(err, &message) {
			return inventory, err
		}
		if errors.Is(err, secrets.ErrStoredEnvelope) {
			return inventory, safetyError("data_envelope", nil)
		}
		return inventory, verificationError(safetyError("data_inspection", err))
	}
	if err := inspectDomainData(ctx, db, version, cipher); err != nil {
		return inventory, err
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
