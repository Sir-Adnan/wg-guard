package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/backup"
	"github.com/Sir-Adnan/wg-guard/internal/config"
	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/token"
)

// runToken manages REST API tokens (docs/architecture/api.md). Until the
// web panel ships (Phase 5) this CLI is the way tokens are minted.
//
//	wg-guard token create -name ci -scopes users.read,users.bulk -expires-in 720h
//	wg-guard token list
//	wg-guard token revoke tok_xxx
//	wg-guard token scopes
//
// The plaintext token is printed exactly once, never logged, never stored.
func runToken(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: wg-guard token <create|list|revoke|scopes> [flags]")
	}
	switch args[0] {
	case "create":
		return tokenCreate(args[1:])
	case "list":
		return tokenList(args[1:])
	case "revoke":
		return tokenRevoke(args[1:])
	case "scopes":
		for _, s := range auth.AllScopes() {
			fmt.Println(s)
		}
		return nil
	default:
		return fmt.Errorf("unknown token command %q", args[0])
	}
}

func tokenCreate(args []string) error {
	var (
		name       string
		scopesRaw  string
		expiresIn  time.Duration
		cidr       string
		configPath = "/etc/wg-guard/wg-guard.toml"
	)
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-config", "--config":
			i++
			configPath = args[i]
		case "-name", "--name":
			i++
			name = args[i]
		case "-scopes", "--scopes":
			i++
			scopesRaw = args[i]
		case "-expires-in", "--expires-in":
			i++
			d, err := time.ParseDuration(args[i])
			if err != nil || d <= 0 {
				return fmt.Errorf("-expires-in must be a positive duration (e.g. 720h)")
			}
			expiresIn = d
		case "-cidr", "--cidr":
			i++
			cidr = args[i]
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}
	if name == "" {
		return fmt.Errorf("-name is required (what is this token for?)")
	}
	var scopes []string
	for _, s := range strings.Split(scopesRaw, ",") {
		if s = strings.TrimSpace(s); s != "" {
			scopes = append(scopes, s)
		}
	}
	if len(scopes) == 0 {
		return fmt.Errorf("-scopes is required (comma-separated; `wg-guard token scopes` lists them; " +
			"least privilege is the rule — never mint tokens wider than the integration needs)")
	}
	var expiresAt *time.Time
	if expiresIn > 0 {
		t := time.Now().UTC().Add(expiresIn)
		expiresAt = &t
	}

	db, closeDB, err := openForToken(configPath)
	if err != nil {
		return err
	}
	defer closeDB()

	t, plaintext, err := token.NewService(db).Create(context.Background(), name, scopes, expiresAt, cidr)
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "Store this token now — it is shown once and cannot be recovered:")
	fmt.Println(plaintext)
	fmt.Fprintf(os.Stderr, "id: %s  scopes: %s\n", t.ID, strings.Join(t.Scopes, ","))
	return nil
}

func tokenList(args []string) error {
	configPath := "/etc/wg-guard/wg-guard.toml"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-config", "--config":
			i++
			configPath = args[i]
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}
	db, closeDB, err := openForToken(configPath)
	if err != nil {
		return err
	}
	defer closeDB()

	tokens, err := token.NewService(db).List(context.Background())
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tPREFIX\tENABLED\tSCOPES\tEXPIRES\tLAST USED")
	for _, t := range tokens {
		exp := "-"
		if t.ExpiresAt != nil {
			exp = t.ExpiresAt.Format(time.RFC3339)
		}
		last := "-"
		if t.LastUsed != nil {
			last = t.LastUsed.Format(time.RFC3339)
		}
		fmt.Fprintf(w, "%s\t%s\t%s…\t%t\t%s\t%s\t%s\n",
			t.ID, t.Name, t.Prefix, t.Enabled, strings.Join(t.Scopes, ","), exp, last)
	}
	return w.Flush()
}

func tokenRevoke(args []string) error {
	configPath := "/etc/wg-guard/wg-guard.toml"
	var id string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-config", "--config":
			i++
			configPath = args[i]
		default:
			id = args[i]
		}
	}
	if id == "" {
		return fmt.Errorf("usage: wg-guard token revoke <token-id>")
	}
	db, closeDB, err := openForToken(configPath)
	if err != nil {
		return err
	}
	defer closeDB()

	if err := token.NewService(db).Revoke(context.Background(), id); err != nil {
		return err
	}
	fmt.Printf("token %s revoked\n", id)
	return nil
}

// openForToken opens the database for token administration. The token
// service needs nothing else: no backend, no boot, no keys beyond the DB.
// Migrations run here (idempotent) so the command works on a fresh node —
// before the first `serve` has ever run.
func openForToken(configPath string) (*database.DB, func(), error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, nil, err
	}
	lease, err := (&backup.Service{Cfg: cfg}).OpenData(false)
	if err != nil {
		return nil, nil, err
	}
	owned := false
	defer func() {
		if !owned {
			lease.Close()
		}
	}()
	if err := os.MkdirAll(filepath.Dir(cfg.DatabasePath), 0o755); err != nil {
		return nil, nil, domain.Wrap(err, domain.CodeConfigInvalid, "create data dir")
	}
	db, err := database.Open(cfg.DatabasePath, database.Options{})
	if err != nil {
		return nil, nil, domain.Wrap(err, domain.CodeConfigInvalid, "open database %s", cfg.DatabasePath)
	}
	if err := db.Migrate(context.Background(), nil); err != nil {
		_ = db.Close()
		return nil, nil, err
	}
	owned = true
	return db, func() { _ = db.Close(); lease.Close() }, nil
}
