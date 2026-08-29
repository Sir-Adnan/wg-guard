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
  serve       Run the WG-Guard service (Phase 4)
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
		fmt.Fprintln(os.Stderr, "wg-guard: serve is not implemented yet (Phase 4)")
		os.Exit(1)
	default:
		fmt.Fprintf(os.Stderr, "wg-guard: unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
}
