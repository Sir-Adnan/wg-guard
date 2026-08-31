package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"golang.org/x/term"

	"github.com/Sir-Adnan/wg-guard/internal/audit"
	"github.com/Sir-Adnan/wg-guard/internal/backup"
	"github.com/Sir-Adnan/wg-guard/internal/config"
	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
	"github.com/Sir-Adnan/wg-guard/internal/settings"
	"github.com/Sir-Adnan/wg-guard/internal/version"
)

// cliEnv is everything the ops commands (backup/restore/settings) share:
// boot config, open database, key ring, settings registry.
type cliEnv struct {
	Cfg        *config.Config
	ConfigPath string
	DB         *database.DB
	Reg        *settings.Registry
	Ring       *secrets.KeyRing
}

// loadCLIEnv loads the boot config and opens the node state. It is the
// ops-command counterpart of runReconcile's setup.
func loadCLIEnv(configPath string) (*cliEnv, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}
	db, err := database.Open(cfg.DatabasePath, database.Options{})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	quiet := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	if err := db.Migrate(context.Background(), quiet); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	ring, err := secrets.LoadKeyRing(cfg.MasterKeyFile)
	if err != nil {
		db.Close()
		return nil, err
	}
	reg, err := settings.New(db, ring, settings.Defaults())
	if err != nil {
		db.Close()
		return nil, err
	}
	return &cliEnv{Cfg: cfg, ConfigPath: configPath, DB: db, Reg: reg, Ring: ring}, nil
}

func (e *cliEnv) Close() { e.DB.Close() }

// newBackupService builds the archive engine over the CLI environment.
func (e *cliEnv) newBackupService() *backup.Service {
	return &backup.Service{
		DB: e.DB, Reg: e.Reg, Audit: audit.NewService(e.DB),
		Cfg: e.Cfg, ConfigPath: e.ConfigPath, Version: version.String(),
	}
}

// readPassword reads a passphrase without echo when stdin is a terminal,
// falling back to a plain read when piped (documented behavior).
func readPassword(prompt string) (string, error) {
	if term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprint(os.Stderr, prompt)
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	b, err := io.ReadAll(io.LimitReader(os.Stdin, 4<<10))
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(b), "\r\n"), nil
}

// runBackup manages archives: create, list, telegram-test.
//
//	wg-guard backup create [-password] [-output DIR]
//	wg-guard backup list
//	wg-guard backup telegram-test
func runBackup(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: wg-guard backup <create|list|telegram-test> [flags]")
	}
	switch args[0] {
	case "create":
		return backupCreate(args[1:])
	case "list":
		return backupList(args[1:])
	case "telegram-test":
		return backupTelegramTest(args[1:])
	default:
		return fmt.Errorf("unknown backup command %q", args[0])
	}
}

func backupCreate(args []string) error {
	var (
		password   string
		output     string
		reason     string
		configPath = "/etc/wg-guard/wg-guard.toml"
	)
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-config", "--config":
			i++
			configPath = args[i]
		case "-password", "--password":
			p, err := readPassword("Archive password (min 8 chars): ")
			if err != nil {
				return fmt.Errorf("read password: %w", err)
			}
			password = p
		case "-output", "--output":
			i++
			output = args[i]
		case "-reason", "--reason":
			i++
			reason = args[i]
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}
	if reason == "" {
		reason = "manual"
	} else if len(reason) > 64 {
		// Reasons land in the archive manifest and audit metadata.
		return fmt.Errorf("reason must be at most 64 characters")
	}

	env, err := loadCLIEnv(configPath)
	if err != nil {
		return err
	}
	defer env.Close()

	res, err := env.newBackupService().Create(context.Background(), backup.CreateOpts{
		Password: password,
		Reason:   reason,
		Deliver:  true,
		Dir:      output,
	})
	if err != nil {
		return err
	}
	fmt.Printf("created %s (%d KiB", res.Name, res.Size>>10)
	if res.Encrypted {
		fmt.Print(", age-encrypted")
	}
	if len(res.Delivered) > 0 {
		fmt.Printf(", delivered to %s", strings.Join(res.Delivered, ", "))
	}
	fmt.Println(")")
	for _, w := range res.Warnings {
		fmt.Printf("warning: %s\n", w)
	}
	return nil
}

func backupList(args []string) error {
	var configPath = "/etc/wg-guard/wg-guard.toml"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-config", "--config":
			i++
			configPath = args[i]
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}
	env, err := loadCLIEnv(configPath)
	if err != nil {
		return err
	}
	defer env.Close()
	ctx := context.Background()

	svc := env.newBackupService()
	arcs, err := svc.List()
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ARCHIVE\tSIZE\tCREATED\tENCRYPTED")
	for _, a := range arcs {
		fmt.Fprintf(tw, "%s\t%d KiB\t%s\t%v\n",
			a.Name, a.Size>>10, a.ModTime.Local().Format("2006-01-02 15:04"), a.Encrypted)
	}
	tw.Flush()

	schedules, err := svc.Schedules(ctx)
	if err != nil {
		return err
	}
	if len(schedules) == 0 {
		return nil
	}
	fmt.Println()
	fmt.Fprintln(tw, "SCHEDULE\tKIND\tRETENTION\tENABLED\tLAST RUN\tNEXT RUN")
	for _, sc := range schedules {
		last := "never"
		if sc.LastRunAt != nil {
			last = sc.LastRunAt.Local().Format("2006-01-02 15:04") + " (" + sc.LastStatus + ")"
		}
		fmt.Fprintf(tw, "%s\t%s\t%d\t%v\t%s\t%s\n",
			sc.Name, describeSchedule(sc), sc.RetentionCount, sc.Enabled, last,
			sc.NextRunAt.Local().Format("2006-01-02 15:04"))
	}
	return tw.Flush()
}

func describeSchedule(sc *backup.Schedule) string {
	switch sc.Kind {
	case backup.KindInterval:
		return fmt.Sprintf("every %dh", sc.IntervalHours)
	case backup.KindWeekly:
		return fmt.Sprintf("%s@%s UTC", time.Weekday(sc.Weekday).String(), sc.TimeOfDay)
	default:
		return sc.TimeOfDay + " UTC"
	}
}

func backupTelegramTest(args []string) error {
	var configPath = "/etc/wg-guard/wg-guard.toml"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-config", "--config":
			i++
			configPath = args[i]
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}
	env, err := loadCLIEnv(configPath)
	if err != nil {
		return err
	}
	defer env.Close()
	ctx := context.Background()

	token, _ := env.Reg.GetSecret(ctx, "backup.telegram_token")
	chat, _ := env.Reg.GetString(ctx, "backup.telegram_chat")
	if token == "" || chat == "" {
		return fmt.Errorf("telegram is not configured: set backup.telegram_token and backup.telegram_chat (wg-guard settings set …)")
	}
	tg := &backup.TelegramSink{Token: token, Chat: chat}
	if err := tg.TestDelivery(ctx); err != nil {
		return fmt.Errorf("delivery failed: %w", err)
	}
	fmt.Println("telegram delivery verified — check the chat for the test file")
	return nil
}

// runRestore applies an archive: verify → stage → (service stopped) apply.
// The engine refuses to run while the node is serving; a staged restore
// applies at the next boot instead.
//
//	wg-guard restore /path/archive.wgg [-password] [-yes]
func runRestore(args []string) error {
	var (
		archive    string
		password   string
		yes        bool
		configPath = "/etc/wg-guard/wg-guard.toml"
	)
	rest := args
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "-config", "--config":
			i++
			configPath = rest[i]
		case "-password", "--password":
			p, err := readPassword("Archive password: ")
			if err != nil {
				return fmt.Errorf("read password: %w", err)
			}
			password = p
		case "-yes", "--yes":
			yes = true
		default:
			if archive == "" && !strings.HasPrefix(rest[i], "-") {
				archive = rest[i]
			} else {
				return fmt.Errorf("unexpected argument %q", rest[i])
			}
		}
	}
	if archive == "" {
		return fmt.Errorf("usage: wg-guard restore ARCHIVE [-password] [-yes]")
	}

	env, err := loadCLIEnv(configPath)
	if err != nil {
		return err
	}
	defer env.Close()
	ctx := context.Background()

	svc := env.newBackupService()
	pr, report, err := svc.Stage(ctx, archive, password)
	if err != nil {
		return err
	}
	printRestoreReport(pr, report)

	if !yes {
		fmt.Print("\nApply this restore now? Type the archive name to confirm: ")
		var answer string
		if _, err := fmt.Scanln(&answer); err != nil || answer != pr.Archive {
			fmt.Println("restore cancelled (the verified staging was discarded)")
			_ = svc.DiscardPending()
			return nil
		}
	}
	if backup.ServiceRunning(env.Cfg.HTTPListen) {
		fmt.Println("\nThe service is running — the verified staging is KEPT and will")
		fmt.Println("apply automatically at the next service restart (before the")
		fmt.Println("database is opened). The current state is snapshotted as *.pre-restore.")
		return nil
	}
	// Release our own database handle before the swap (the apply is pure
	// file replacement; the audit record reopens the fresh database).
	env.Close()

	applied, err := svc.ApplyStaged(ctx)
	if err != nil {
		return err
	}
	fmt.Println("\nrestore applied. Safety copies: <database>.pre-restore, <master key>.pre-restore.")
	for _, w := range applied.Warnings {
		fmt.Println("note: " + w)
	}
	fmt.Println("start the service to bring the restored state up (reconcile runs at boot).")
	if db, err := database.Open(env.Cfg.DatabasePath, database.Options{}); err == nil {
		_ = audit.NewService(db).Record(ctx, audit.Entry{
			ActorType: audit.ActorSystem, Action: "backup.restored",
			Target: pr.Archive, Metadata: map[string]any{"applied_at": "cli"},
		})
		db.Close()
	}
	return nil
}

func printRestoreReport(pr *backup.PendingRestore, report *backup.RestoreReport) {
	fmt.Println("restore verified — environment review")
	fmt.Printf("  archive:        %s (%d KiB%s)\n", pr.Archive, pr.Size>>10, encryptedSuffix(report))
	if !report.CreatedAt.IsZero() {
		fmt.Printf("  created:        %s on %s (app %s)\n",
			report.CreatedAt.Local().Format("2006-01-02 15:04"), report.Hostname, report.AppVersion)
	}
	fmt.Printf("  node id:        %s\n", orDash(report.NodeID))
	fmt.Printf("  endpoint:       %s\n", orDash(report.Endpoint))
	if report.TLSMode != "" {
		fmt.Printf("  archived tls:   mode=%s listen=%s\n", report.TLSMode, report.Listen)
	}
	for _, f := range report.Interfaces {
		fmt.Printf("  interface:      %s port=%d subnet=%s\n", f.Name, f.Port, f.Subnet)
	}
	for _, w := range report.Warnings {
		fmt.Println("  warning:        " + w)
	}
	fmt.Println("  review the values above — client configs embed the endpoint;")
	fmt.Println("  edits happen after apply through Settings (or the staged config).")
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func encryptedSuffix(report *backup.RestoreReport) string {
	if report.Encrypted {
		return ", age-encrypted"
	}
	return ""
}
