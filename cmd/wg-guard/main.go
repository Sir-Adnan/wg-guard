// Command wg-guard is the single binary of the WG-Guard panel:
// HTTP server (web panel + REST API), scheduler, accounting, and
// AmneziaWG tunnel management. See docs/ for the architecture.
package main

import (
	"fmt"
	"os"

	"github.com/Sir-Adnan/wg-guard/internal/version"
)

const usage = `wg-guard — lightweight AmneziaWG VPN node management panel

Usage:
  wg-guard <command> [flags]

Commands:
  version     Print version information
  reconcile   Bring tunnels, peers, and firewall to DB state (boot bring-up)
  serve       Run the WG-Guard service (API + scheduler)
              -config PATH   boot config (default /etc/wg-guard/wg-guard.toml)
              -backend fake  dev/benchmark backend: no tunnels, no host changes
  token       Manage REST API tokens
              token create -name NAME -scopes a,b [-expires-in 720h] [-cidr LIST]
              token list | token revoke ID | token scopes
  backup      Manage archives (docs/operations/backup-restore.md)
              backup create [-password] [-output DIR] | backup list
              backup telegram-test
  restore     Restore an archive (verifies, reviews, applies with the
              service stopped; staged otherwise)
              restore ARCHIVE [-password] [-yes]
  settings    Read/write runtime settings (secrets are never printed)
              settings list | settings get KEY | settings set KEY VALUE
  doctor      Health checks + safe repairs
              doctor [-fix]
  secrets     Master-key rotation (service must be stopped)
              secrets rotate [-yes]
  help        Show this help
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	switch os.Args[1] {
	case "version":
		fmt.Println(version.String())
	case "help", "-h", "--help":
		fmt.Print(usage)
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
