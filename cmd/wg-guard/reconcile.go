package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/backup"
	"github.com/Sir-Adnan/wg-guard/internal/boot"
	"github.com/Sir-Adnan/wg-guard/internal/config"
	"github.com/Sir-Adnan/wg-guard/internal/database"
	"github.com/Sir-Adnan/wg-guard/internal/firewall"
	"github.com/Sir-Adnan/wg-guard/internal/secrets"
	"github.com/Sir-Adnan/wg-guard/internal/settings"
	"github.com/Sir-Adnan/wg-guard/internal/shaper"
	"github.com/Sir-Adnan/wg-guard/internal/subprocess"
	"github.com/Sir-Adnan/wg-guard/internal/tunnel/amneziawg"
)

// runReconcile brings the node to DB state outside the service: tooling
// probe, IPv4 forwarding, tunnel/peer reconciliation, firewall table, and
// firewall-manager coexistence (docs/architecture/networking.md). It is the
// same orchestration `serve` runs at startup (internal/boot).
func runReconcile(args []string) error {
	configPath := "/etc/wg-guard/wg-guard.toml"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-config", "--config":
			if i+1 >= len(args) {
				return fmt.Errorf("-config requires a path")
			}
			i++
			configPath = args[i]
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	if err := (&backup.Service{Cfg: cfg}).CheckOpen(); err != nil {
		return err
	}
	db, err := database.Open(cfg.DatabasePath, database.Options{})
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()
	quiet := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	if err := db.Migrate(context.Background(), quiet); err != nil {
		return err
	}
	ring, err := secrets.LoadKeyRing(cfg.MasterKeyFile)
	if err != nil {
		return err
	}
	reg, err := settings.New(db, ring, settings.Defaults())
	if err != nil {
		return err
	}

	runner := subprocess.NewSystem()
	backend := amneziawg.New(runner)

	res, err := boot.BringUp(context.Background(), boot.Deps{
		DB:       db,
		Ring:     ring,
		Settings: reg,
		Backend:  backend,
		Run:      runner,
		Shaper:   shaper.New(runner),
	})
	if err != nil {
		return err
	}
	printReconcileReport(res)
	if len(res.Reconcile.Errors) > 0 {
		return fmt.Errorf("%d interface(s) failed reconciliation (see drift/errors above)",
			len(res.Reconcile.Errors))
	}
	return nil
}

// printReconcileReport renders a human-readable summary. Nothing here logs
// configuration content — only counts, names, and versions.
func printReconcileReport(res *boot.Result) {
	if res.ToolsVersionMatchesPin {
		fmt.Printf("awg tools: %s (matches pin)\n", res.ToolsVersion)
	} else {
		fmt.Printf("awg tools: %s (pin: %s — verify before relying on behavior)\n",
			res.ToolsVersion, amneziawg.PinnedToolsVersion)
	}
	if res.ForwardingChanged {
		fmt.Println("ipv4 forwarding: enabled (was off)")
	} else {
		fmt.Println("ipv4 forwarding: already enabled")
	}

	rep := res.Reconcile
	fmt.Printf("tunnels: %d created, %d updated, %d removed; peers: %d added, %d updated, %d removed (%s)\n",
		rep.InterfacesCreated, rep.InterfacesUpdated, rep.InterfacesRemoved,
		rep.PeersAdded, rep.PeersUpdated, rep.PeersRemoved,
		rep.Duration.Round(time.Millisecond))
	for _, d := range rep.Drift {
		fmt.Printf("  drift: [%s] %s: %s (%s)\n", d.Interface, d.Kind, d.Detail, d.Action)
	}
	for _, e := range rep.Errors {
		fmt.Printf("  error: [%s] %s\n", e.Interface, e.Err)
	}

	fmt.Printf("firewall: table applied for %d interface(s)\n", res.ManagedIfaces)
	if res.ShapedGroups > 0 {
		fmt.Printf("shaper: %d speed-limited group(s) ensured\n", res.ShapedGroups)
	}
	if len(res.UfwRoutes) > 0 {
		fmt.Printf("ufw: route rules ensured for %s\n", strings.Join(res.UfwRoutes, ", "))
	}
	for _, f := range res.Findings {
		switch f.Tool {
		case "tc":
			fmt.Printf("shaper: %s\n  remedy: %s\n", f.Detail, f.Remedy)
		default:
			fmt.Printf("coexistence: %s active%s\n  remedy: %s\n", f.Tool, blockingSuffix(f), f.Remedy)
		}
	}
	fmt.Println("reconcile complete")
}

func blockingSuffix(f firewall.Finding) string {
	if f.Blocking {
		return " (forward policy may drop tunnel traffic)"
	}
	return ""
}
