package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/config"
	"github.com/Sir-Adnan/wg-guard/internal/install"
	"github.com/Sir-Adnan/wg-guard/internal/version"
)

// routeDockerMode dispatches commands on a docker-mode host: the host binary
// is the mode-aware shim (ADR-0006). Panel/data commands exec into the
// container — same binary, same volume layout, identical CLI in both modes.
// It runs before command dispatch and returns only for host-side commands.
func routeDockerMode() {
	if os.Getenv("WGG_IN_CONTAINER") == "1" {
		return
	}
	st, err := install.LoadState(install.NewRealHost())
	if err != nil || st == nil || st.Mode != install.ModeDocker {
		return
	}
	cmd := os.Args[1]
	switch install.Route(cmd) {
	case "host":
		return
	case "refuse":
		fmt.Fprintf(os.Stderr, "wg-guard: 'wg-guard %s' runs inside the container; manage it with:\n"+
			"  docker compose -f %s <up -d|down|restart|logs>\n", cmd, install.ComposePth)
		os.Exit(2)
	}
	argv := append([]string{"docker", "exec", "-i", install.Container, install.BinPath}, os.Args[1:]...)
	host := install.NewRealHost()
	if err := host.Run(context.Background(), argv, 10*time.Minute); err != nil {
		fmt.Fprintln(os.Stderr, "wg-guard:", err)
		os.Exit(1)
	}
	os.Exit(0)
}

func runInstall(args []string) error {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	var (
		mode      = fs.String("mode", "", "docker (default) | native")
		domain    = fs.String("domain", "", "panel domain (enables ACME TLS)")
		tlsMode   = fs.String("tls", "", "acme | manual | proxy | dev (default: acme with domain, dev without)")
		panelPort = fs.Int("panel-port", 0, "panel port (default 443 with TLS, 8080 plain)")
		acmePort  = fs.Int("acme-http-port", 80, "ACME HTTP-01 challenge port (acme mode)")
		image     = fs.String("image", install.DefaultImage, "container image (docker mode)")
		certFile  = fs.String("cert-file", "", "TLS certificate file (manual mode)")
		keyFile   = fs.String("key-file", "", "TLS key file (manual mode)")
		yes       = fs.Bool("yes", false, "non-interactive: flags + defaults, no confirmation")
		skipMod   = fs.Bool("skip-module", false, "do not attempt the host AmneziaWG kernel-module install")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}

	plan := install.Defaults()
	if *mode != "" {
		plan.Mode = install.Mode(*mode)
	}
	plan.Domain = *domain
	if *tlsMode != "" {
		plan.TLSMode = config.TLSMode(*tlsMode)
	}
	if *panelPort != 0 {
		plan.PanelPort = *panelPort
	}
	plan.ACMEHTTPPort = *acmePort
	plan.Image = *image
	plan.CertFile = *certFile
	plan.KeyFile = *keyFile

	_, err := install.Install(context.Background(), install.NewRealHost(), install.InstallOptions{
		Plan:       plan,
		Yes:        *yes,
		Version:    version.Version,
		SkipModule: *skipMod,
		Stdin:      os.Stdin,
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
	})
	return err
}

func runUninstall(args []string) error {
	fs := flag.NewFlagSet("uninstall", flag.ExitOnError)
	var (
		dryRun    = fs.Bool("dry-run", false, "print the plan without changing anything")
		purgeData = fs.Bool("purge-data", false, "also delete /var/lib/wg-guard (database, keys, backups) — default keeps it")
		purgePkgs = fs.Bool("purge-packages", false, "also remove packages the installer installed (kernel module)")
		yes       = fs.Bool("yes", false, "do not ask for confirmation")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	_, err := install.Uninstall(context.Background(), install.NewRealHost(), install.UninstallOptions{
		DryRun:        *dryRun,
		PurgeData:     *purgeData,
		PurgePackages: *purgePkgs,
		Yes:           *yes,
		Stdin:         os.Stdin,
		Stdout:        os.Stdout,
		Stderr:        os.Stderr,
	})
	return err
}

func runUpdate(args []string) error {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	var (
		image      = fs.String("image", "", "new image reference (docker mode)")
		binaryPath = fs.String("binary", "", "staged new binary path (native mode)")
		skipBackup = fs.Bool("skip-backup", false, "skip the pre-upgrade backup (not recommended)")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	return install.Update(context.Background(), install.NewRealHost(), install.UpdateOptions{
		Image:      *image,
		BinaryPath: *binaryPath,
		SkipBackup: *skipBackup,
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
	})
}

// runStatus prints the operational snapshot: version, install state, service
// state and health.
func runStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	h := install.NewRealHost()
	ctx := context.Background()

	fmt.Println(version.String())
	st, err := install.LoadState(h)
	if err != nil {
		return err
	}
	if st == nil {
		fmt.Println("install:     not installed (no install state; run wg-guard install)")
		return nil
	}
	fmt.Printf("install:     %s mode, created %s\n", st.Mode, st.CreatedAt)
	if st.Image != "" {
		fmt.Printf("image:       %s\n", st.Image)
	}

	fmt.Print("service:     ")
	switch st.Mode {
	case install.ModeDocker:
		// The container's own docker status line (e.g. "Up 2 minutes
		// (healthy)") is what an operator wants here.
		stat, err := h.Output(ctx, []string{"docker", "ps",
			"--filter", "name=^/" + install.Container + "$", "--format", "{{.Status}}"}, 30*time.Second)
		stat = strings.TrimSpace(stat)
		if err != nil || stat == "" {
			fmt.Println("down")
			return nil
		}
		fmt.Println(stat)
	default:
		if err := h.Run(ctx, []string{"systemctl", "is-active", "--quiet", "wg-guard"}, 30*time.Second); err != nil {
			fmt.Println("inactive")
			return nil
		}
		fmt.Println("active")
	}

	cfg, err := install.ReadBootConfig(h, st.ConfigPath)
	if err != nil {
		return err
	}
	p := install.Plan{
		TLSMode:      cfg.TLS.Mode,
		Domain:       cfg.TLS.Domain,
		ACMEHTTPPort: cfg.TLS.ACMEHTTPPort,
	}
	if _, port, err := splitListen(cfg.HTTPListen); err == nil {
		p.PanelPort = port
	}
	if p.ACMEHTTPPort == 0 {
		p.ACMEHTTPPort = 80
	}
	url, skipVerify, err := p.HealthProbeURL()
	if err != nil {
		return err
	}
	if err := install.ProbeHealth(ctx, url, skipVerify); err != nil {
		fmt.Printf("health:      UNHEALTHY (%v)\n", err)
	} else {
		fmt.Printf("health:      ok (%s)\n", p.PanelURL())
	}
	return nil
}

func splitListen(listen string) (host string, port int, err error) {
	idx := strings.LastIndex(listen, ":")
	if idx < 0 {
		return "", 0, fmt.Errorf("no port in %q", listen)
	}
	n := 0
	for _, c := range listen[idx+1:] {
		if c < '0' || c > '9' {
			return "", 0, fmt.Errorf("bad port in %q", listen)
		}
		n = n*10 + int(c-'0')
	}
	return listen[:idx], n, nil
}
