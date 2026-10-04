package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/backup"
	"github.com/Sir-Adnan/wg-guard/internal/device"
	"github.com/Sir-Adnan/wg-guard/internal/iface"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
	"github.com/Sir-Adnan/wg-guard/internal/subscription"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel"
	"github.com/Sir-Adnan/wg-guard/internal/user"
	"github.com/Sir-Adnan/wg-guard/internal/webhook"
)

func TestSecretsRotateRejectsMissingConfigValueBeforeOpen(t *testing.T) {
	for _, args := range [][]string{
		{"rotate", "--config"},
		{"rotate", "-config"},
		{"rotate", "--config", "--yes"},
		{"rotate", "--config", ""},
	} {
		err := runSecrets(args)
		if err == nil || !(strings.Contains(err.Error(), "requires a path") || strings.Contains(err.Error(), "flag needs an argument")) {
			t.Fatalf("runSecrets(%q) = %v, want missing path error", args, err)
		}
	}
}

func TestSecretsRotatePreservesAllNodeFieldsAndPortableBackup(t *testing.T) {
	ctx := context.Background()
	path := testTokenConfig(t)
	env, err := loadCLIEnv(path)
	if err != nil {
		t.Fatal(err)
	}
	defer env.Close()
	profile, err := iface.NewService(env.DB, env.Reg, env.Ring).Create(ctx, iface.CreateInput{Name: "awg0", Subnet: "10.77.0.0/23"})
	if err != nil {
		t.Fatal(err)
	}
	account, err := user.NewService(env.DB).Create(ctx, user.Input{Username: "rotation-fixture"})
	if err != nil {
		t.Fatal(err)
	}
	devices := device.NewService(env.DB, env.Ring)
	// More than one rotation page, including PSKs in the binary field.
	for index := range 129 {
		pair, err := tunnel.GenerateKeyPair()
		if err != nil {
			t.Fatal(err)
		}
		private, err := env.Ring.Encrypt([]byte(pair.Private))
		if err != nil {
			t.Fatal(err)
		}
		shared, err := tunnel.GeneratePresharedKey()
		if err != nil {
			t.Fatal(err)
		}
		psk, err := env.Ring.Encrypt([]byte(shared))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := devices.Create(ctx, account.ID, fmt.Sprintf("device-%d", index), device.KeyMaterial{PublicKey: pair.Public, PrivateKeyEnc: private, PresharedEnc: psk}, profile.ID); err != nil {
			t.Fatal(err)
		}
	}
	link, err := subscription.NewService(env.DB, env.Ring).Ensure(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	hook, _, err := webhook.NewService(env.DB, env.Ring).Create(ctx, "https://hooks.example/rotation", []string{webhook.EventUserCreated}, "synthetic-webhook-secret")
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{"backup.password": "synthetic-password", "backup.telegram_token": "synthetic-token"} {
		if err := env.Reg.SetRaw(ctx, key, value); err != nil {
			t.Fatal(err)
		}
	}
	before, _ := os.ReadFile(env.Cfg.MasterKeyFile)
	keyPath := env.Cfg.MasterKeyFile
	env.Close()
	if err := runSecrets([]string{"rotate", "--config", path, "--yes"}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(keyPath)
	if bytes.Equal(before, after) {
		t.Fatal("new rotation did not change the master key")
	}
	if _, err := os.Stat(keyPath + secrets.KeyFileSuffixPrev); !os.IsNotExist(err) {
		t.Fatal("successful rotation kept its predecessor key")
	}
	reloaded, err := loadCLIEnv(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.Close()
	count := 0
	if err := secrets.WalkStoredSecrets(ctx, reloaded.DB.DB, "", false, func(value []byte) error {
		count++
		plaintext, err := reloaded.Ring.Decrypt(value)
		clear(plaintext)
		return err
	}); err != nil || count != 263 {
		t.Fatal("new key does not cover all stored fields and pages")
	}
	newLink, err := subscription.NewService(reloaded.DB, reloaded.Ring).ForUser(ctx, account.ID)
	if err != nil || newLink == nil || newLink.Token != link.Token {
		t.Fatal("master-key rotation changed or lost the customer capability")
	}
	if value, err := webhook.NewService(reloaded.DB, reloaded.Ring).Secret(ctx, hook); err != nil || value != "synthetic-webhook-secret" {
		t.Fatal("master-key rotation lost webhook delivery credentials")
	}
	if err := reloaded.Reg.SetRaw(ctx, "backup.password", ""); err != nil {
		t.Fatal(err)
	}
	archive, err := reloaded.newBackupService().Create(ctx, backup.CreateOpts{})
	if err != nil {
		t.Fatal("post-rotation backup failed", err)
	}
	if report, err := backup.VerifyArchive(ctx, archive.Path, ""); err != nil || report.Inventory.Devices != 129 {
		t.Fatal("post-rotation archive is not independently portable")
	}
}
