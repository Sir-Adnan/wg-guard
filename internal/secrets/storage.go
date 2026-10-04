package secrets

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// MaxStoredEnvelopeBytes bounds one encrypted database value, including text encoding.
const MaxStoredEnvelopeBytes = 8 << 10

var ErrStoredEnvelope = errors.New("secrets: invalid stored envelope")

// These are concrete node storage fields, not a protocol/plugin registry. SQL
// identifiers and predicates are closed source constants, never request data.
// Startup sampling, portable backup verification and rotation use the same list.
type storedField struct {
	table, column, id, since, predicate string
	text, optional                      bool
}

var secretSettingKeys = [...]string{"backup.password", "backup.telegram_token"}

var nodeFields = [...]storedField{
	{table: "tunnel_interfaces", column: "private_key_encrypted", id: "id"},
	{table: "devices", column: "private_key_encrypted", id: "id"},
	{table: "devices", column: "preshared_key_encrypted", id: "id", optional: true},
	{table: "sub_links", column: "token_encrypted", id: "user_id", since: "0004_sub_links.sql"},
	{table: "webhook_endpoints", column: "secret_encrypted", id: "id", text: true},
	{table: "settings", column: "value", id: "key", text: true,
		predicate: "key IN ('" + strings.Join(secretSettingKeys[:], "','") + "')"},
}

// SecretSettingKeys lets the settings catalog enforce parity with stored inspection.
func SecretSettingKeys() []string { return append([]string(nil), secretSettingKeys[:]...) }

func (f storedField) selectSQL() string {
	value := "CAST(" + f.column + " AS BLOB)"
	id := "CASE WHEN length(CAST(" + f.id + " AS BLOB))<=128 THEN " + f.id + " ELSE '' END"
	return "SELECT " + id + ", length(" + value + "), CASE WHEN length(" + value + ")<=? THEN " + value + " END FROM " + f.table
}

func (f storedField) whereSQL() string {
	if f.predicate != "" {
		return " WHERE " + f.predicate
	}
	return ""
}

func (f storedField) decode(size sql.NullInt64, encoded []byte) ([]byte, error) {
	if f.optional && (!size.Valid || size.Int64 == 0) {
		return nil, nil
	}
	if !size.Valid || size.Int64 <= 0 || size.Int64 > MaxStoredEnvelopeBytes {
		return nil, ErrStoredEnvelope
	}
	if !f.text {
		return encoded, nil
	}
	if !bytes.HasPrefix(encoded, []byte("enc:")) {
		return nil, ErrStoredEnvelope
	}
	decoded := make([]byte, base64.StdEncoding.DecodedLen(len(encoded)-4))
	n, err := base64.StdEncoding.Decode(decoded, encoded[4:])
	if err != nil {
		clear(decoded)
		return nil, ErrStoredEnvelope
	}
	return decoded[:n], nil
}

// WalkStoredSecrets streams bounded binary envelopes. schema="" means current
// schema; older snapshots skip fields added later. sample selects at most one
// value per field (nonempty for optional fields), not a full integrity claim.
// The visitor must not retain the value: it is cleared immediately after return.
func WalkStoredSecrets(ctx context.Context, db *sql.DB, schema string, sample bool, visit func([]byte) error) error {
	for _, field := range nodeFields {
		if schema != "" && schema < field.since {
			continue
		}
		query := field.selectSQL() + field.whereSQL()
		if sample && field.optional {
			if field.predicate == "" {
				query += " WHERE "
			} else {
				query += " AND "
			}
			query += "length(CAST(" + field.column + " AS BLOB))>0"
		}
		if sample {
			query += " LIMIT 1"
		}
		rows, err := db.QueryContext(ctx, query, MaxStoredEnvelopeBytes)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			var size sql.NullInt64
			var encoded []byte
			if err = rows.Scan(&id, &size, &encoded); err == nil {
				var envelope []byte
				if id == "" {
					err = ErrStoredEnvelope
				} else {
					envelope, err = field.decode(size, encoded)
				}
				if err == nil && envelope != nil {
					err = visit(envelope)
				}
				clear(envelope)
			}
			clear(encoded)
			if err != nil {
				rows.Close()
				return err
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	return ctx.Err()
}

type nodeCarrier struct {
	ctx context.Context
	db  *sql.DB
}

// NodeCarrier rotates all concrete encrypted node fields. The caller owns the
// exclusive DB/key lease and keeps the service stopped for the complete rotation.
func NodeCarrier(ctx context.Context, db *sql.DB) Carrier { return nodeCarrier{ctx, db} }

func (c nodeCarrier) ReencryptSecrets(from, to *Cipher) error {
	for _, field := range nodeFields {
		if err := c.rotateField(field, from, to); err != nil {
			return err
		}
	}
	if err := WalkStoredSecrets(c.ctx, c.db, "", false, func(value []byte) error {
		plaintext, err := to.Decrypt(value)
		clear(plaintext)
		return err
	}); err != nil {
		return err
	}
	// With SQLite synchronous=NORMAL, checkpoint must flush the completed
	// envelope writes before the retained predecessor key can be removed.
	var busy, frames, checkpointed int
	if err := c.db.QueryRowContext(c.ctx, "PRAGMA wal_checkpoint(FULL)").Scan(&busy, &frames, &checkpointed); err != nil {
		return err
	}
	if busy != 0 || frames != checkpointed {
		return fmt.Errorf("secrets: rotation checkpoint incomplete; retained key preserved")
	}
	return nil
}

func (c nodeCarrier) rotateField(field storedField, from, to *Cipher) error {
	// Collect only one bounded page before writing: a live read cursor cannot
	// share this connection's transaction, and a whole-table slice is unbounded.
	type update struct {
		id    string
		value any
	}
	after := ""
	for {
		query := field.selectSQL() + field.whereSQL()
		if field.predicate == "" {
			query += " WHERE "
		} else {
			query += " AND "
		}
		query += "(" + field.id + ">? OR ?='') ORDER BY " + field.id + " LIMIT 128"
		rows, err := c.db.QueryContext(c.ctx, query, MaxStoredEnvelopeBytes, after, after)
		if err != nil {
			return err
		}
		var updates []update
		count := 0
		for rows.Next() {
			count++
			var id string
			var size sql.NullInt64
			var encoded []byte
			if err = rows.Scan(&id, &size, &encoded); err != nil {
				rows.Close()
				return err
			}
			if id == "" || len(id) > 128 {
				rows.Close()
				return ErrStoredEnvelope
			}
			after = id
			value, err := rotateStoredValue(field, size, encoded, from, to)
			if err != nil {
				rows.Close()
				return err
			}
			if value != nil {
				updates = append(updates, update{id, value})
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(updates) > 0 {
			tx, err := c.db.BeginTx(c.ctx, nil)
			if err != nil {
				return err
			}
			for _, item := range updates {
				if _, err := tx.ExecContext(c.ctx, "UPDATE "+field.table+" SET "+field.column+"=? WHERE "+field.id+"=?", item.value, item.id); err != nil {
					_ = tx.Rollback()
					return fmt.Errorf("secrets: stored rotation write failed: %w", err)
				}
			}
			if err := tx.Commit(); err != nil {
				return err
			}
		}
		if count < 128 {
			return nil
		}
	}
}

func rotateStoredValue(field storedField, size sql.NullInt64, encoded []byte, from, to *Cipher) (any, error) {
	defer clear(encoded)
	envelope, err := field.decode(size, encoded)
	if err != nil || envelope == nil {
		return nil, err
	}
	defer clear(envelope)
	// Already-current rows remain unchanged when completing a dual-key window.
	plaintext, err := to.Decrypt(envelope)
	clear(plaintext)
	if err == nil {
		return nil, nil
	}
	plaintext, err = from.Decrypt(envelope)
	if err != nil {
		return nil, ErrTampered
	}
	sealed, err := to.Encrypt(plaintext)
	clear(plaintext)
	if err != nil {
		return nil, err
	}
	if field.text {
		value := "enc:" + base64.StdEncoding.EncodeToString(sealed)
		clear(sealed)
		return value, nil
	}
	return sealed, nil
}
