package settings

import (
	"bytes"
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
)

func key32(b byte) []byte { return bytes.Repeat([]byte{b}, 32) }

func newRegistry(t *testing.T) (*Registry, *database.DB) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "s.db"), database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	ring, err := secrets.LoadKeyRing(filepath.Join(t.TempDir(), "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	reg, err := New(db, ring, Defaults())
	if err != nil {
		t.Fatal(err)
	}
	return reg, db
}

func TestDefaultsServedWithoutRows(t *testing.T) {
	reg, _ := newRegistry(t)
	ctx := context.Background()
	if mtu, err := reg.GetInt(ctx, "network.mtu"); err != nil || mtu != 1420 {
		t.Fatalf("default mtu = %d, %v", mtu, err)
	}
	dns, err := reg.GetStringList(ctx, "network.dns_servers")
	if err != nil || len(dns) != 2 || dns[0] != "1.1.1.1" {
		t.Fatalf("default dns = %v, %v", dns, err)
	}
	policy, _ := reg.GetString(ctx, "drift.policy")
	if policy != "report" {
		t.Fatalf("default drift policy = %q", policy)
	}
	if _, err := reg.Get(ctx, "no.such.key"); domain.CodeOf(err) != domain.CodeSettingUnknown {
		t.Fatalf("unknown key must be SETTING_UNKNOWN, got %v", err)
	}
}

func TestSetRawValidation(t *testing.T) {
	reg, _ := newRegistry(t)
	ctx := context.Background()
	cases := []struct {
		key, raw string
		wantCode string // "" = success
	}{
		{"network.mtu", "1280", ""},
		{"network.mtu", "100", domain.CodeSettingInvalid},   // below min
		{"network.mtu", "99999", domain.CodeSettingInvalid}, // above max
		{"network.mtu", "fast", domain.CodeSettingInvalid},
		{"drift.policy", "remove", ""},
		{"drift.policy", "nuke", domain.CodeSettingInvalid}, // not in options
		{"network.dns_servers", "9.9.9.9, 149.112.112.112", ""},
		{"network.dns_servers", "not-an-ip", domain.CodeSettingInvalid},
		{"security.session_idle_hours", "24", ""},
		{"security.session_idle_hours", "0", domain.CodeSettingInvalid},
		{"backup.telegram_chat", "123456789", ""},
		{"backup.telegram_chat", "chat123", domain.CodeSettingInvalid},
		{"interfaces.max_count", "16", ""},
	}
	for _, tc := range cases {
		err := reg.SetRaw(ctx, tc.key, tc.raw)
		if tc.wantCode == "" {
			if err != nil {
				t.Errorf("%s=%s: unexpected error %v", tc.key, tc.raw, err)
			}
			continue
		}
		if domain.CodeOf(err) != tc.wantCode {
			t.Errorf("%s=%s: want %s, got %v", tc.key, tc.raw, tc.wantCode, err)
		}
	}
	// Invalid writes must not change the effective value: the last *valid*
	// write (1280) is still in effect.
	if mtu, _ := reg.GetInt(ctx, "network.mtu"); mtu != 1280 {
		t.Fatalf("invalid write leaked: mtu=%d", mtu)
	}
}

func TestSecretsEncryptedAtRestAndRedacted(t *testing.T) {
	reg, db := newRegistry(t)
	ctx := context.Background()
	if err := reg.Set(ctx, "backup.password", "hunter2-but-optional"); err != nil {
		t.Fatalf("set secret: %v", err)
	}
	if got, err := reg.GetSecret(ctx, "backup.password"); err != nil || got != "hunter2-but-optional" {
		t.Fatalf("secret round trip: %q %v", got, err)
	}
	var stored string
	if err := db.QueryRow(`SELECT value FROM settings WHERE key='backup.password'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !secrets.IsEncryptedText(stored) {
		t.Fatal("secret stored in plaintext")
	}
	if got, _ := reg.Get(ctx, "backup.password"); got == "hunter2-but-optional" {
		t.Fatal("plain Get returned secret plaintext")
	}
	items, _ := reg.All(ctx)
	for _, it := range items {
		if it.Key == "backup.password" && it.Value != "<set>" {
			t.Fatalf("All() leaked secret value: %v", it.Value)
		}
	}
	// Unsetting (empty) returns to the default and clears the row.
	if err := reg.Set(ctx, "backup.password", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := reg.GetSecret(ctx, "backup.password"); got != "" {
		t.Fatalf("expected empty after unset, got %q", got)
	}
}

func TestResetRestoresDefault(t *testing.T) {
	reg, _ := newRegistry(t)
	ctx := context.Background()
	if err := reg.Set(ctx, "interfaces.max_count", 4); err != nil {
		t.Fatal(err)
	}
	if n, _ := reg.GetInt(ctx, "interfaces.max_count"); n != 4 {
		t.Fatalf("override not applied: %d", n)
	}
	if err := reg.Reset(ctx, "interfaces.max_count"); err != nil {
		t.Fatal(err)
	}
	if n, _ := reg.GetInt(ctx, "interfaces.max_count"); n != 8 {
		t.Fatalf("reset did not restore default: %d", n)
	}
}

func TestCacheInvalidationAndConcurrency(t *testing.T) {
	reg, _ := newRegistry(t)
	ctx := context.Background()

	var wg sync.WaitGroup
	errCh := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				if err := reg.Set(ctx, "accounting.interval_seconds", 15+j%10); err != nil {
					errCh <- err
					return
				}
				if _, err := reg.GetInt(ctx, "accounting.interval_seconds"); err != nil {
					errCh <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent access: %v", err)
	}
	if n, err := reg.GetInt(ctx, "accounting.interval_seconds"); err != nil || n < 15 || n > 24 {
		t.Fatalf("final value out of written range: %d %v", n, err)
	}
}

func TestReencryptSecrets(t *testing.T) {
	reg, db := newRegistry(t)
	ctx := context.Background()
	if err := reg.Set(ctx, "backup.telegram_token", "bot-token"); err != nil {
		t.Fatal(err)
	}
	old, err := secrets.NewCipher(key32(1))
	if err != nil {
		t.Fatal(err)
	}
	neu, err := secrets.NewCipher(key32(2))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.ReencryptSecrets(old, neu); err == nil {
		t.Fatal("rotation with a foreign old key must fail loudly")
	}

	// Simulate a rotation where the registry's rows were written by a ring
	// whose key we rotate: build a second registry on the same DB with its
	// own ring, then rotate through that ring.
	reg2, _ := registryWithFreshRing(t, db)
	if err := reg2.Set(ctx, "backup.telegram_token", "bot-token"); err != nil {
		t.Fatal(err)
	}
	ringNew, err := secrets.NewCipher(key32(3))
	if err != nil {
		t.Fatal(err)
	}
	// Decrypt-with-old then encrypt-with-new needs the old key: recover it by
	// re-writing the row with a known cipher, exercising the carrier.
	knownOld, _ := secrets.NewCipher(key32(4))
	enc, _ := knownOld.EncryptString("bot-token")
	if _, err := db.Exec(`UPDATE settings SET value=? WHERE key='backup.telegram_token'`, enc); err != nil {
		t.Fatal(err)
	}
	if err := reg2.ReencryptSecrets(knownOld, ringNew); err != nil {
		t.Fatalf("reencrypt: %v", err)
	}
	var stored string
	if err := db.QueryRow(`SELECT value FROM settings WHERE key='backup.telegram_token'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if got, err := ringNew.DecryptString(stored); err != nil || got != "bot-token" {
		t.Fatalf("new key cannot decrypt re-encrypted row: %v %q", err, got)
	}
}

func registryWithFreshRing(t *testing.T, db *database.DB) (*Registry, error) {
	t.Helper()
	ring, err := secrets.LoadKeyRing(filepath.Join(t.TempDir(), "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	return New(db, ring, Defaults())
}

func TestDefinitionsAreSane(t *testing.T) {
	reg, _ := newRegistry(t)
	seen := map[string]bool{}
	for _, d := range reg.Definitions() {
		if seen[d.Key] {
			t.Fatalf("duplicate definition %s", d.Key)
		}
		seen[d.Key] = true
		if d.Category == "" {
			t.Fatalf("%s missing category", d.Key)
		}
	}
	if !seen["network.mtu"] || !seen["backup.password"] {
		t.Fatal("expected key definitions missing")
	}
}
