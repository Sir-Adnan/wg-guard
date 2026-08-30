// Package settings is the typed runtime-settings registry: a code-defined
// catalog of keys with kinds, defaults, and validators, backed by the
// `settings` table. Boot-level configuration lives in TOML (internal/config);
// everything an administrator may change while the node runs lives here.
// Values the docs mark as "recommended defaults" (MTU, ports, DNS, interface
// cap) are ordinary registry entries an administrator can override.
package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
)

type Kind int

const (
	KindString Kind = iota
	KindInt
	KindBool
	KindStringList // JSON array of strings
	KindSecret     // stored encrypted with the master key (never logged)
)

func (k Kind) String() string {
	switch k {
	case KindInt:
		return "int"
	case KindBool:
		return "bool"
	case KindStringList:
		return "string_list"
	case KindSecret:
		return "secret"
	default:
		return "string"
	}
}

// Definition is one registry entry. Min/Max apply to KindInt; Options
// restrict KindString to an enum; Validator adds cross-field or structural
// rules on the typed value.
type Definition struct {
	Key        string
	Kind       Kind
	Default    any
	Min, Max   int64
	Options    []string
	Validator  func(v any) error
	Category   string // general|networking|accounting|security|backup|drift|interfaces
	Secret     bool
	Advanced   bool // hidden behind the Settings UI "advanced" gate
	Persistent bool // change requires the owning feature to re-render state
}

// Item is a resolved setting returned to UIs/CLI.
type Item struct {
	Definition
	Value   any
	IsZero  bool // value equals the default (row absent)
	Updated time.Time
}

// Registry is safe for concurrent use. Values are cached in memory and
// invalidated on write; this is a read-heavy, write-rare table.
type Registry struct {
	db    *database.DB
	ring  *secrets.KeyRing
	defs  map[string]Definition
	mu    sync.RWMutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	val     any
	updated time.Time
}

func New(db *database.DB, ring *secrets.KeyRing, defs []Definition) (*Registry, error) {
	if ring == nil {
		return nil, fmt.Errorf("settings: key ring required")
	}
	r := &Registry{
		db:    db,
		ring:  ring,
		defs:  make(map[string]Definition, len(defs)),
		cache: map[string]cacheEntry{},
	}
	for _, d := range defs {
		if d.Key == "" || d.Category == "" {
			return nil, fmt.Errorf("settings: definition %q: key and category are required", d.Key)
		}
		switch d.Kind {
		case KindInt:
			if _, ok := d.Default.(int); !ok {
				return nil, fmt.Errorf("settings: %s: int default required", d.Key)
			}
		case KindBool:
			if _, ok := d.Default.(bool); !ok {
				return nil, fmt.Errorf("settings: %s: bool default required", d.Key)
			}
		case KindString, KindSecret:
			if d.Default != nil {
				if _, ok := d.Default.(string); !ok {
					return nil, fmt.Errorf("settings: %s: string default required", d.Key)
				}
			}
		case KindStringList:
			if _, ok := d.Default.([]string); !ok {
				return nil, fmt.Errorf("settings: %s: []string default required", d.Key)
			}
		}
		r.defs[d.Key] = d
	}
	return r, nil
}

// Definitions returns the catalog (order undefined).
func (r *Registry) Definitions() []Definition {
	out := make([]Definition, 0, len(r.defs))
	for _, d := range r.defs {
		out = append(out, d)
	}
	return out
}

// Get returns the typed value (default when no row). Unknown keys error.
func (r *Registry) Get(ctx context.Context, key string) (any, error) {
	def, ok := r.defs[key]
	if !ok {
		return nil, domain.E(domain.CodeSettingUnknown, "unknown setting %q", key)
	}
	r.mu.RLock()
	if ce, ok := r.cache[key]; ok {
		r.mu.RUnlock()
		return ce.val, nil
	}
	r.mu.RUnlock()

	var raw, rawTS sql.NullString
	err := r.db.QueryRowContext(ctx, `SELECT value, updated_at FROM settings WHERE key = ?`, key).
		Scan(&raw, &rawTS)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("settings: load %s: %w", key, err)
	}
	var (
		val     any
		updated time.Time
	)
	if !raw.Valid {
		val = def.Default
	} else {
		val, err = r.decode(def, raw.String)
		if err != nil {
			return nil, err
		}
		if rawTS.Valid {
			updated, _ = time.Parse(time.RFC3339Nano, rawTS.String)
		}
	}
	// Secrets are redacted on the generic path; plaintext leaves only through
	// GetSecret (callers there own the not-logging obligation).
	if def.Kind == KindSecret {
		if s, _ := val.(string); s != "" {
			val = "<set>"
		} else {
			val = ""
		}
	}
	r.mu.Lock()
	r.cache[key] = cacheEntry{val: val, updated: updated}
	r.mu.Unlock()
	return val, nil
}

// Typed getters. A wrong-kind expectation is a programmer error surfaced as
// an error, not a panic.
func (r *Registry) GetString(ctx context.Context, key string) (string, error) {
	v, err := r.Get(ctx, key)
	if err != nil {
		return "", err
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("settings: %s is not a string", key)
	}
	return s, nil
}

func (r *Registry) GetInt(ctx context.Context, key string) (int, error) {
	v, err := r.Get(ctx, key)
	if err != nil {
		return 0, err
	}
	i, ok := v.(int)
	if !ok {
		return 0, fmt.Errorf("settings: %s is not an int", key)
	}
	return i, nil
}

func (r *Registry) GetBool(ctx context.Context, key string) (bool, error) {
	v, err := r.Get(ctx, key)
	if err != nil {
		return false, err
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("settings: %s is not a bool", key)
	}
	return b, nil
}

func (r *Registry) GetStringList(ctx context.Context, key string) ([]string, error) {
	v, err := r.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	l, ok := v.([]string)
	if !ok {
		return nil, fmt.Errorf("settings: %s is not a string list", key)
	}
	return l, nil
}

// GetSecret returns the decrypted secret value ("" when unset). It is the
// only accessor that yields plaintext; the value must never be logged.
func (r *Registry) GetSecret(ctx context.Context, key string) (string, error) {
	def, ok := r.defs[key]
	if !ok || def.Kind != KindSecret {
		return "", fmt.Errorf("settings: %s is not a secret setting", key)
	}
	var raw sql.NullString
	err := r.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		d, _ := def.Default.(string)
		return d, nil
	}
	if err != nil {
		return "", fmt.Errorf("settings: load %s: %w", key, err)
	}
	if raw.String == "" {
		return "", nil
	}
	return r.ring.DecryptString(raw.String)
}

// SetRaw validates and stores a textual input (CLI/API form); parsing is
// kind-driven. Secret values are plaintext on input, encrypted at rest.
func (r *Registry) SetRaw(ctx context.Context, key, raw string) error {
	def, ok := r.defs[key]
	if !ok {
		return domain.E(domain.CodeSettingUnknown, "unknown setting %q", key)
	}
	var stored string
	switch def.Kind {
	case KindInt:
		n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil {
			return domain.E(domain.CodeSettingInvalid, "%s: %q is not an integer", key, raw)
		}
		stored = strconv.FormatInt(n, 10)
	case KindBool:
		switch strings.ToLower(strings.TrimSpace(raw)) {
		case "true", "1", "yes", "on":
			stored = "true"
		case "false", "0", "no", "off":
			stored = "false"
		default:
			return domain.E(domain.CodeSettingInvalid, "%s: %q is not a boolean", key, raw)
		}
	case KindStringList:
		var parts []string
		for _, p := range strings.Split(raw, ",") {
			if p = strings.TrimSpace(p); p != "" {
				parts = append(parts, p)
			}
		}
		b, err := json.Marshal(parts)
		if err != nil {
			return domain.E(domain.CodeSettingInvalid, "%s: encode list: %v", key, err)
		}
		stored = string(b)
	default: // string, secret
		stored = raw
	}
	return r.store(ctx, def, stored)
}

// Set validates and stores a typed value (panel form).
func (r *Registry) Set(ctx context.Context, key string, value any) error {
	def, ok := r.defs[key]
	if !ok {
		return domain.E(domain.CodeSettingUnknown, "unknown setting %q", key)
	}
	var stored string
	switch def.Kind {
	case KindInt:
		n, ok := toInt(value)
		if !ok {
			return domain.E(domain.CodeSettingInvalid, "%s: expected integer, got %T", key, value)
		}
		stored = strconv.FormatInt(int64(n), 10)
	case KindBool:
		b, ok := value.(bool)
		if !ok {
			return domain.E(domain.CodeSettingInvalid, "%s: expected bool, got %T", key, value)
		}
		stored = strconv.FormatBool(b)
	case KindStringList:
		l, ok := value.([]string)
		if !ok {
			return domain.E(domain.CodeSettingInvalid, "%s: expected []string, got %T", key, value)
		}
		b, err := json.Marshal(l)
		if err != nil {
			return domain.E(domain.CodeSettingInvalid, "%s: encode list: %v", key, err)
		}
		stored = string(b)
	default:
		s, ok := value.(string)
		if !ok {
			return domain.E(domain.CodeSettingInvalid, "%s: expected string, got %T", key, value)
		}
		stored = s
	}
	return r.store(ctx, def, stored)
}

// store runs the full validation pipeline and persists (secrets encrypted).
func (r *Registry) store(ctx context.Context, def Definition, stored string) error {
	// Secret values arrive as plaintext and are encrypted below; every other
	// kind is decoded from its stored textual form before validation.
	typed, err := func() (any, error) {
		if def.Kind == KindSecret {
			return stored, nil
		}
		return r.decode(def, stored)
	}()
	if err != nil {
		return err
	}
	if err := r.validateValue(def, typed); err != nil {
		return err
	}
	var final string
	if def.Kind == KindSecret {
		if stored == "" {
			final = "" // unset secret: the default ("") applies
		} else {
			enc, err := r.ring.EncryptString(stored)
			if err != nil {
				return fmt.Errorf("settings: encrypt %s: %w", def.Key, err)
			}
			final = enc
		}
	} else {
		final = stored
	}
	if final == "" {
		// Empty value → the default applies; drop the row so Get serves it.
		if _, err := r.db.ExecContext(ctx, `DELETE FROM settings WHERE key = ?`, def.Key); err != nil {
			return fmt.Errorf("settings: delete %s: %w", def.Key, err)
		}
	} else if _, err := r.db.ExecContext(ctx, `
		INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		def.Key, final, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("settings: upsert %s: %w", def.Key, err)
	}
	r.mu.Lock()
	delete(r.cache, def.Key)
	r.mu.Unlock()
	return nil
}

// Reset removes an override; the default applies again.
func (r *Registry) Reset(ctx context.Context, key string) error {
	if _, ok := r.defs[key]; !ok {
		return domain.E(domain.CodeSettingUnknown, "unknown setting %q", key)
	}
	if _, err := r.db.ExecContext(ctx, `DELETE FROM settings WHERE key = ?`, key); err != nil {
		return fmt.Errorf("settings: reset %s: %w", key, err)
	}
	r.mu.Lock()
	delete(r.cache, key)
	r.mu.Unlock()
	return nil
}

// Validate checks a value against the key's constraints WITHOUT persisting —
// the panel uses it to validate form input before committing a sequence of
// writes (e.g. onboarding validates node.endpoint before creating the owner).
func (r *Registry) Validate(key string, value any) error {
	def, ok := r.defs[key]
	if !ok {
		return domain.E(domain.CodeSettingUnknown, "unknown setting %q", key)
	}
	switch def.Kind {
	case KindInt:
		n, ok := toInt(value)
		if !ok {
			return domain.E(domain.CodeSettingInvalid, "%s: expected integer, got %T", key, value)
		}
		return r.validateValue(def, n)
	default:
		s, ok := value.(string)
		if !ok {
			return domain.E(domain.CodeSettingInvalid, "%s: expected string, got %T", key, value)
		}
		return r.validateValue(def, s)
	}
}

// All resolves every definition (settings screens). Secret values are
// redacted to "<set>"/"" — never returned in plaintext.
func (r *Registry) All(ctx context.Context) ([]Item, error) {
	defs := r.Definitions()
	out := make([]Item, 0, len(defs))
	for _, d := range defs {
		v, err := r.Get(ctx, d.Key)
		if err != nil {
			return nil, err
		}
		if d.Kind == KindSecret {
			if s, _ := v.(string); s != "" {
				v = "<set>"
			} else {
				v = ""
			}
		}
		item := Item{Definition: d, Value: v}
		if fmt.Sprint(v) == fmt.Sprint(d.Default) {
			item.IsZero = true
		}
		r.mu.RLock()
		if ce, ok := r.cache[d.Key]; ok {
			item.Updated = ce.updated
		}
		r.mu.RUnlock()
		out = append(out, item)
	}
	return out, nil
}

// ReencryptSecrets re-encrypts every stored secret row from `from` to `to`
// (master-key rotation carrier, secrets.Carrier contract).
func (r *Registry) ReencryptSecrets(from, to *secrets.Cipher) error {
	rows, err := r.db.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return fmt.Errorf("settings: rotate scan: %w", err)
	}
	type pair struct{ key, newVal string }
	var updates []pair
	for rows.Next() {
		var key, val string
		if err := rows.Scan(&key, &val); err != nil {
			rows.Close()
			return fmt.Errorf("settings: rotate: %w", err)
		}
		if !secrets.IsEncryptedText(val) {
			continue
		}
		pt, err := from.DecryptString(val)
		if err != nil {
			rows.Close()
			return fmt.Errorf("settings: rotate %s: %w", key, err)
		}
		enc, err := to.EncryptString(pt)
		if err != nil {
			rows.Close()
			return fmt.Errorf("settings: rotate %s: %w", key, err)
		}
		updates = append(updates, pair{key, enc})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("settings: rotate: %w", err)
	}
	rows.Close()

	for _, u := range updates {
		if _, err := r.db.Exec(`UPDATE settings SET value = ? WHERE key = ?`, u.newVal, u.key); err != nil {
			return fmt.Errorf("settings: rotate write %s: %w", u.key, err)
		}
	}
	r.mu.Lock()
	r.cache = map[string]cacheEntry{} // encrypted values changed wholesale
	r.mu.Unlock()
	return nil
}

// decode converts a stored TEXT value into its typed form (decrypting
// secrets).
func (r *Registry) decode(def Definition, stored string) (any, error) {
	switch def.Kind {
	case KindInt:
		n, err := strconv.ParseInt(stored, 10, 64)
		if err != nil {
			return nil, domain.E(domain.CodeSettingInvalid, "%s: stored value %q is not an integer", def.Key, stored)
		}
		return int(n), nil
	case KindBool:
		b, err := strconv.ParseBool(stored)
		if err != nil {
			return nil, domain.E(domain.CodeSettingInvalid, "%s: stored value %q is not a boolean", def.Key, stored)
		}
		return b, nil
	case KindStringList:
		var l []string
		if err := json.Unmarshal([]byte(stored), &l); err != nil {
			return nil, domain.E(domain.CodeSettingInvalid, "%s: stored value is not a JSON list", def.Key)
		}
		return l, nil
	case KindSecret:
		if stored == "" {
			return "", nil
		}
		pt, err := r.ring.DecryptString(stored)
		if err != nil {
			return nil, fmt.Errorf("settings: decrypt %s: %w", def.Key, err)
		}
		return pt, nil
	default:
		return stored, nil
	}
}

func (r *Registry) validateValue(def Definition, typed any) error {
	switch def.Kind {
	case KindInt:
		n := int64(typed.(int))
		if n < def.Min || n > def.Max {
			return domain.E(domain.CodeSettingInvalid, "%s must be between %d and %d, got %d",
				def.Key, def.Min, def.Max, n)
		}
	case KindString:
		if def.Options != nil {
			s := typed.(string)
			for _, o := range def.Options {
				if s == o {
					return nil
				}
			}
			return domain.E(domain.CodeSettingInvalid, "%s must be one of: %s", def.Key, strings.Join(def.Options, ", "))
		}
	}
	if def.Validator != nil {
		if err := def.Validator(typed); err != nil {
			return domain.E(domain.CodeSettingInvalid, "%s: %v", def.Key, err)
		}
	}
	return nil
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}
