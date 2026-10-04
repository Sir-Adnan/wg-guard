package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/config"
	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/nodestate"
	"github.com/Sir-Adnan/wg-guard/internal/token"
	"github.com/Sir-Adnan/wg-guard/internal/version"
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
		if len(args) != 1 {
			return fmt.Errorf("usage: wg-guard token scopes")
		}
		for _, s := range auth.AllScopes() {
			fmt.Println(s)
		}
		return nil
	default:
		return fmt.Errorf("unknown token command %q", args[0])
	}
}

func tokenCreate(args []string) error {
	flags, configPath := tokenFlags("token create")
	name := flags.String("name", "", "token name")
	scopesRaw := flags.String("scopes", "", "comma-separated scopes")
	expiresIn := flags.Duration("expires-in", 0, "positive lifetime")
	cidr := flags.String("cidr", "", "client CIDR")
	if err := parseTokenFlags(flags, args, false, configPath); err != nil {
		return err
	}
	if *name == "" {
		return fmt.Errorf("-name is required (what is this token for?)")
	}
	var scopes []string
	for _, s := range strings.Split(*scopesRaw, ",") {
		if s = strings.TrimSpace(s); s != "" {
			scopes = append(scopes, s)
		}
	}
	if len(scopes) == 0 {
		return fmt.Errorf("-scopes is required (comma-separated; `wg-guard token scopes` lists them; " +
			"least privilege is the rule — never mint tokens wider than the integration needs)")
	}
	var expiresAt *time.Time
	if *expiresIn < 0 {
		return fmt.Errorf("-expires-in must be a positive duration (e.g. 720h)")
	}
	if *expiresIn > 0 {
		t := time.Now().UTC().Add(*expiresIn)
		expiresAt = &t
	}

	db, closeDB, err := openForToken(*configPath)
	if err != nil {
		return err
	}
	defer closeDB()

	t, plaintext, err := token.NewService(db).Create(context.Background(), *name, scopes, expiresAt, *cidr)
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "Store this token now — it is shown once and cannot be recovered:")
	fmt.Println(plaintext)
	fmt.Fprintf(os.Stderr, "id: %s  scopes: %s\n", t.ID, strings.Join(t.Scopes, ","))
	return nil
}

func tokenList(args []string) error {
	flags, configPath := tokenFlags("token list")
	if err := parseTokenFlags(flags, args, false, configPath); err != nil {
		return err
	}
	db, closeDB, err := openForToken(*configPath)
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
	flags, configPath := tokenFlags("token revoke")
	if err := parseTokenFlags(flags, args, true, configPath); err != nil {
		return err
	}
	id := flags.Arg(0)
	if id == "" {
		return fmt.Errorf("usage: wg-guard token revoke <token-id>")
	}
	db, closeDB, err := openForToken(*configPath)
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

func tokenFlags(name string) (*flag.FlagSet, *string) {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	return flags, flags.String("config", "/etc/wg-guard/wg-guard.toml", "node configuration")
}

func parseTokenFlags(flags *flag.FlagSet, args []string, positional bool, configPath *string) error {
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("%s: %w", flags.Name(), err)
	}
	if (!positional && flags.NArg() != 0) || (positional && flags.NArg() > 1) {
		return fmt.Errorf("%s: unexpected argument", flags.Name())
	}
	if strings.TrimSpace(*configPath) == "" || strings.HasPrefix(*configPath, "-") {
		return fmt.Errorf("%s: --config requires a path", flags.Name())
	}
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
	state, err := nodestate.OpenDatabase(context.Background(), nodestate.Options{Config: cfg, ConfigPath: configPath, Version: version.String()})
	if err != nil {
		return nil, nil, err
	}
	return state.DB, func() { _ = state.Close() }, nil
}
