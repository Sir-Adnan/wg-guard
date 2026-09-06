// Command wg-guard is the single binary of the WG-Guard panel:
// HTTP server (web panel + REST API), scheduler, accounting, and
// AmneziaWG tunnel management. See docs/ for the architecture.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/Sir-Adnan/wg-guard/internal/i18n"
	"github.com/Sir-Adnan/wg-guard/internal/install"
	"github.com/Sir-Adnan/wg-guard/internal/version"
)

var usage = `wg-guard — lightweight AmneziaWG VPN node management panel

Usage:
  wg-guard <command> [flags]

Commands:
  version     Print version information
  install     Interactive installer (Docker default, native systemd secondary;
              --yes for non-interactive installs)
              install [--mode docker|native] [--domain D] [--tls acme|manual|proxy|dev]
                      [--panel-port N] [--acme-http-port N] [--cert-file F --key-file F]
                      [--image REF] [--skip-module] [--yes]
  update      Explicit update: pre-upgrade backup, swap, health-checked rollback
              update [--image REF] (docker) | update --binary PATH (native)
  uninstall   Remove WG-Guard (data kept unless --purge-data)
              uninstall [--dry-run] [--purge-data] [--purge-packages] [--yes]
  status      Install state, service state and health
  reconcile   Bring tunnels, peers, and firewall to DB state (boot bring-up)
  serve       Run the WG-Guard service (API + scheduler)
              -config PATH   boot config (default /etc/wg-guard/wg-guard.toml)
              -backend fake  dev/benchmark backend: no tunnels, no host changes
  token       Manage REST API tokens
              token create -name NAME -scopes a,b [-expires-in 720h] [-cidr LIST]
              token list | token revoke ID | token scopes
  backup      Manage archives (docs/operations/backup-restore.md)
              backup create [-password] [-output DIR] [-reason TEXT] | backup list
              backup schedule-add -kind daily -time 03:30 [-name N] [-retention N]
              backup telegram-test
  restore     Restore an archive (verifies, reviews, applies with the
              service stopped; staged otherwise)
              restore ARCHIVE [-password] [-yes]
  settings    Read/write runtime settings (secrets are never printed)
              settings list | settings get KEY | settings set KEY VALUE
              echo SECRET | settings set KEY -stdin   (secret via stdin, not argv)
  doctor      Health checks + safe repairs
              doctor [-fix]
  secrets     Master-key rotation (service must be stopped)
              secrets rotate [-yes]
  help        Show this help
` + i18n.T(i18n.En, "install.cli.help")

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	// Contract probes inspect this binary without reading or migrating node data.
	if os.Args[1] == "installer-contract" {
		if len(os.Args) != 2 {
			os.Exit(2)
		}
		if err := json.NewEncoder(os.Stdout).Encode(install.CurrentContract()); err != nil {
			os.Exit(1)
		}
		return
	}
	// Docker-mode host shim: on a docker-mode install the host binary routes
	// panel/data commands into the container (ADR-0006). No-op otherwise.
	routeDockerMode()

	switch os.Args[1] {
	case "core":
		if err := runCore(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "tls-check":
		if err := runTLSCheck(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "version":
		fmt.Println(version.String())
	case "help", "-h", "--help":
		fmt.Print(usage)
	case "install":
		if err := runInstall(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "wg-guard: install: %v\n", err)
			os.Exit(1)
		}
	case "update":
		if err := runUpdate(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "wg-guard: update: %v\n", err)
			os.Exit(1)
		}
	case "uninstall":
		if err := runUninstall(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "wg-guard: uninstall: %v\n", err)
			os.Exit(1)
		}
	case "status":
		if err := runStatus(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "wg-guard: status: %v\n", err)
			os.Exit(1)
		}
	case "reconcile":
		if err := runReconcile(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "wg-guard: reconcile: %v\n", err)
			os.Exit(1)
		}
	case "serve":
		if err := runServe(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "wg-guard: serve: %v\n", err)
			os.Exit(1)
		}
	case "token":
		if err := runToken(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "wg-guard: token: %v\n", err)
			os.Exit(1)
		}
	case "backup":
		if err := runBackup(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "wg-guard: backup: %v\n", err)
			os.Exit(1)
		}
	case "restore":
		if err := runRestore(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "wg-guard: restore: %v\n", err)
			os.Exit(1)
		}
	case "settings":
		if err := runSettings(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "wg-guard: settings: %v\n", err)
			os.Exit(1)
		}
	case "doctor":
		if err := runDoctor(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "wg-guard: doctor: %v\n", err)
			os.Exit(1)
		}
	case "secrets":
		if err := runSecrets(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "wg-guard: secrets: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "wg-guard: unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
}
