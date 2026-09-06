package main

import (
	"bytes"
	"context"
	"github.com/Sir-Adnan/wg-guard/internal/admin"
	"github.com/Sir-Adnan/wg-guard/internal/config"
	"github.com/Sir-Adnan/wg-guard/internal/database"
	"path/filepath"
	"strings"
	"testing"
)

func TestOwnerUsesExplicitConfigNotInheritedEnvironment(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.Complete()
	path := filepath.Join(dir, "node.toml")
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WGG_DATABASE_PATH", filepath.Join(t.TempDir(), "wrong.db"))
	svc, close, err := loadOwnerService(path)
	if err != nil {
		t.Fatal(err)
	}
	defer close()
	if _, err := svc.BootstrapOwner(context.Background(), "owner", "synthetic-password-123"); err != nil {
		t.Fatal(err)
	}
	db, err := database.Open(cfg.DatabasePath, database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	has, err := admin.NewService(db, nil).HasOwner(context.Background())
	if err != nil || !has {
		t.Fatalf("owner created outside configured service DB: %v", err)
	}
	if _, _, err := loadOwnerService(filepath.Join(dir, "missing.toml")); err == nil {
		t.Fatal("missing config accepted")
	}
}

func TestOwnerBootstrapSharedServiceAndBoundedInput(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "owner.db"), database.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	svc := admin.NewService(db, nil)
	var out bytes.Buffer
	if err := bootstrapOwnerInput(context.Background(), svc, strings.NewReader(`{"username":"root","password":"synthetic-password-123"}`), &out); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(context.Background(), "root", "synthetic-password-123"); err != nil {
		t.Fatal(err)
	}
	if err := bootstrapOwnerInput(context.Background(), svc, strings.NewReader(`{"username":"root","password":"replacement-password"}`), &out); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(context.Background(), "root", "synthetic-password-123"); err != nil {
		t.Fatal("owner reset")
	}
	if strings.Contains(out.String(), "password") {
		t.Fatal("secret output")
	}
	if err := bootstrapOwnerInput(context.Background(), svc, strings.NewReader(strings.Repeat("x", 16385)), &out); err == nil {
		t.Fatal("unbounded input")
	}
}
