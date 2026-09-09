package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/mail"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/config"
	"github.com/Sir-Adnan/wg-guard/internal/distribution"
	"github.com/Sir-Adnan/wg-guard/internal/i18n"
	"github.com/Sir-Adnan/wg-guard/internal/install"
	"github.com/Sir-Adnan/wg-guard/internal/terminal"
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
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if st == nil || st.Mode != install.ModeDocker {
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
	o, err := parseInstallOptions(args)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	h := install.NewRealHost()
	if err := install.CheckLifecycleReady(h); err != nil {
		return err
	}
	if !o.Yes && o.Selection.Channel == "" && o.BuildMetadata == "" {
		u := terminal.New(o.Stdin, o.Stdout, terminal.Detect(o.Stdin, o.Stdout, o.Locale))
		u.Context = ctx
		o.Selection, err = pickSource(ctx, u, distribution.NewClient(nil, distribution.Options{}), version.String())
		if err != nil {
			return err
		}
	}
	build, parent, cleanup, err := prepareBuild(ctx, o.Selection, o.BuildMetadata)
	if err != nil {
		return err
	}
	defer cleanup()
	o.Build = build
	o.StageParent = parent
	o.Version = build.Version
	_, err = install.Install(ctx, h, o)
	return err
}

func runRecoverInstall(args []string) error {
	return runRecoverInstallWith(context.Background(), args, install.NewRealHost(), os.Stdout)
}

func runRecoverInstallWith(ctx context.Context, args []string, h install.Host, out io.Writer) error {
	fs := flag.NewFlagSet("recover-install", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "confirm safe cleanup of an interrupted initial setup")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return lifecycleArgsError()
	}
	return install.CleanupIncompleteInstall(ctx, h, *yes, out)
}

func parseInstallOptions(args []string) (install.InstallOptions, error) {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	var (
		mode           = fs.String("mode", "", "docker (default) | native")
		domain         = fs.String("domain", "", "panel domain (enables ACME TLS)")
		tlsMode        = fs.String("tls", "", "acme | manual | proxy | dev (default: acme with domain, dev without)")
		exposure       = fs.String("exposure", "auto", "auto | private | direct | nginx | external-proxy")
		certificate    = fs.String("certificate", "auto", "auto | builtin | webroot | cloudflare-dns | ip | manual | cloudflare-origin | external")
		panelPort      = fs.Int("panel-port", 0, "panel port (default 443 with TLS, 8080 plain)")
		httpsPort      = fs.Int("https-port", 443, "public HTTPS port for Nginx/external proxy")
		acmePort       = fs.Int("acme-http-port", 80, "ACME HTTP-01 challenge port (acme mode)")
		acmeEmail      = fs.String("acme-email", "", "optional ACME account email")
		cloudflareFile = fs.String("cloudflare-token-file", "", "private 0600 file containing a scoped Cloudflare API token")
		image          = fs.String("image", install.DefaultImage, "container image (docker mode)")
		certFile       = fs.String("cert-file", "", "TLS certificate file (manual mode)")
		keyFile        = fs.String("key-file", "", "TLS key file (manual mode)")
		yes            = fs.Bool("yes", false, "non-interactive: flags + defaults, no confirmation")
		skipMod        = fs.Bool("skip-module", false, i18n.T(i18n.En, "install.cli.skip_module"))
		publicIP       = fs.String("public-ip", "", i18n.T(i18n.En, "install.cli.public_ip"))
		prerequisites  = fs.String("prerequisites", "auto", i18n.T(i18n.En, "install.cli.prerequisites"))
		core           = fs.String("core", "recommended", i18n.T(i18n.En, "install.cli.core"))
		release        = fs.String("release", "", i18n.T(i18n.En, "install.cli.release"))
		commit         = fs.String("commit", "", i18n.T(i18n.En, "install.cli.commit"))
		metadata       = fs.String("build-metadata", "", i18n.T(i18n.En, "install.cli.metadata"))
		localImage     = fs.Bool("local-image", false, i18n.T(i18n.En, "install.cli.local_image"))
		ownerName      = fs.String("owner-username", "", i18n.T(i18n.En, "owner.username"))
		ownerFile      = fs.String("owner-password-file", "", i18n.T(i18n.En, "owner.file"))
		locale         = fs.String("lang", terminalLocale(), "terminal UI language (English; fa is a legacy alias)")
	)
	if err := fs.Parse(args); err != nil {
		return install.InstallOptions{}, err
	}
	if fs.NArg() != 0 {
		return install.InstallOptions{}, fmt.Errorf("%s", i18n.T(i18n.En, "install.cli.arguments"))
	}
	terminalLang, ok := terminalLanguage(*locale)
	if !ok {
		return install.InstallOptions{}, lifecycleArgsError()
	}
	selection, err := sourceSelection(*release, *commit)
	if err != nil {
		return install.InstallOptions{}, err
	}
	if *metadata != "" && selection.Channel != "" {
		return install.InstallOptions{}, lifecycleArgsError()
	}
	if *prerequisites != "auto" && *prerequisites != "check" {
		return install.InstallOptions{}, fmt.Errorf("%s", i18n.T(i18n.En, "install.cli.policy"))
	}
	if _, err := install.SelectCore(*core); err != nil {
		return install.InstallOptions{}, err
	}
	validExposure := map[string]bool{"auto": true, "private": true, "direct": true, "nginx": true, "external-proxy": true}
	validCertificate := map[string]bool{"auto": true, "builtin": true, "webroot": true, "cloudflare-dns": true, "ip": true, "manual": true, "cloudflare-origin": true, "external": true}
	if !validExposure[*exposure] || !validCertificate[*certificate] {
		return install.InstallOptions{}, lifecycleArgsError()
	}
	if *httpsPort < 1 || *httpsPort > 65535 {
		return install.InstallOptions{}, lifecycleArgsError()
	}
	if *acmeEmail != "" {
		address, parseErr := mail.ParseAddress(*acmeEmail)
		if parseErr != nil || address.Address != *acmeEmail || !strings.Contains(*acmeEmail, "@") {
			return install.InstallOptions{}, lifecycleArgsError()
		}
	}
	visited := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { visited[f.Name] = true })
	if visited["tls"] && (visited["exposure"] || visited["certificate"]) {
		return install.InstallOptions{}, fmt.Errorf("install: legacy --tls cannot be combined with --exposure or --certificate")
	}
	if *cloudflareFile != "" && *certificate != string(install.CertificateCloudflareDNS) {
		return install.InstallOptions{}, fmt.Errorf("install: --cloudflare-token-file requires --certificate cloudflare-dns")
	}

	plan := install.Defaults()
	switch {
	case *mode != "":
		plan.Mode = install.Mode(*mode)
	case !*yes:
		// Interactive: the wizard asks for the mode (Enter = Docker default).
		plan.Mode = ""
	} // --yes without --mode keeps the Docker default
	plan.Domain = *domain
	plan.Exposure = install.ExposureMode(*exposure)
	plan.Certificate = install.CertificateSource(*certificate)
	plan.ExposureExplicit = visited["exposure"]
	plan.CertificateExplicit = visited["certificate"]
	plan.PublicPort = *httpsPort
	plan.ACMEEmail = *acmeEmail
	plan.CloudflareTokenFile = *cloudflareFile
	if *cloudflareFile != "" {
		plan.CloudflareToken, err = install.ReadCloudflareToken(install.NewRealHost(), *cloudflareFile)
		if err != nil {
			return install.InstallOptions{}, err
		}
	}
	if *tlsMode != "" {
		plan.TLSMode = config.TLSMode(*tlsMode)
		plan.TLSModeExplicit = true
	}
	if visited["panel-port"] {
		plan.PanelPort = *panelPort
		plan.PanelPortExplicit = true
	}
	plan.ACMEHTTPPort = *acmePort
	plan.Image = *image
	plan.CertFile = *certFile
	plan.KeyFile = *keyFile
	plan.PublicIP = *publicIP

	return install.InstallOptions{
		Owner: install.OwnerOptions{Username: *ownerName, PasswordFile: *ownerFile}, Locale: terminalLang,
		Selection: selection, BuildMetadata: *metadata, LocalImage: *localImage,
		Plan:          plan,
		Yes:           *yes,
		Version:       version.Version,
		SkipModule:    *skipMod,
		Prerequisites: install.PrerequisitePolicy(*prerequisites),
		Core:          *core,
		Stdin:         os.Stdin,
		Stdout:        os.Stdout,
		Stderr:        os.Stderr,
	}, nil
}

func runUninstall(args []string) error {
	fs := flag.NewFlagSet("uninstall", flag.ExitOnError)
	var (
		dryRun    = fs.Bool("dry-run", false, "print the plan without changing anything")
		purgeData = fs.Bool("purge-data", false, "also delete /var/lib/wg-guard (database, keys, backups) — default keeps it")
		purgePkgs = fs.Bool("purge-packages", false, "also remove packages the installer installed (kernel module)")
		purgeAll  = fs.Bool("purge-all", false, "remove all exclusively owned WG-Guard files, data, packages, manager cache and logs")
		yes       = fs.Bool("yes", false, "do not ask for confirmation")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	_, err := install.Uninstall(ctx, install.NewRealHost(), install.UninstallOptions{
		DryRun:        *dryRun,
		PurgeData:     *purgeData,
		PurgePackages: *purgePkgs,
		PurgeAll:      *purgeAll,
		Yes:           *yes,
		Stdin:         os.Stdin,
		Stdout:        os.Stdout,
		Stderr:        os.Stderr,
	})
	return err
}

func runPanelUpdate(args []string) error {
	o, err := parseUpdateOptions(args)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	h := install.NewRealHost()
	u := terminal.New(nil, os.Stdout, terminal.Detect(nil, os.Stdout, i18n.En))
	if !o.Recover {
		if err := install.CheckLifecycleReady(h); err != nil {
			return err
		}
	}
	if o.Selection.Channel != "" {
		stopHeartbeat := startUpdateHeartbeat(ctx, os.Stdout, "Acquiring and verifying the selected WG-Guard build…")
		build, parent, cleanup, err := prepareBuild(ctx, o.Selection, "")
		stopHeartbeat()
		if err != nil {
			return err
		}
		defer cleanup()
		o.Build = build
		o.BinaryPath = build.BinaryPath
		u.Success("Verified build ready.")
		if err := install.UpdateManager(ctx, h, install.ManagerUpdateOptions{Build: build, Stdout: os.Stdout}); err != nil {
			return fmt.Errorf("cache verified manager before panel update: %w", err)
		}
		st, err := install.LoadState(h)
		if err != nil {
			return err
		}
		if st != nil && st.Mode == install.ModeDocker {
			u.Info("Building the local Docker runtime…")
			bundle, err := install.SelectCore(st.Core.Requested.ID)
			if err != nil {
				return err
			}
			o.Image, err = install.BuildRuntimeImage(ctx, h, build, bundle, parent)
			if err != nil {
				return err
			}
			o.LocalImage = true
			u.Success("Docker runtime ready.")
		}
	}
	return install.Update(ctx, h, o)
}
func parseUpdateOptions(args []string) (install.UpdateOptions, error) {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	var (
		image      = fs.String("image", "", "new image reference (docker mode)")
		binaryPath = fs.String("binary", "", "staged new binary path (native mode)")
		skipBackup = fs.Bool("skip-backup", false, "skip the pre-upgrade backup (not recommended)")
		rollback   = fs.Bool("rollback", false, "re-deploy the last healthy image/binary recorded in the install state (recovery after a failed or interrupted update)")
		recover    = fs.Bool("recover", false, i18n.T(i18n.En, "install.cli.recover"))
		local      = fs.Bool("local-image", false, i18n.T(i18n.En, "install.cli.local_image"))
		release    = fs.String("release", "", i18n.T(i18n.En, "install.cli.release"))
		commit     = fs.String("commit", "", i18n.T(i18n.En, "install.cli.commit"))
	)
	if err := fs.Parse(args); err != nil {
		return install.UpdateOptions{}, err
	}
	selection, err := sourceSelection(*release, *commit)
	if err != nil {
		return install.UpdateOptions{}, err
	}
	if fs.NArg() != 0 || (*rollback || *recover) && (selection.Channel != "" || *binaryPath != "" || *image != "" || *local || *skipBackup) || *rollback && *recover || selection.Channel != "" && (*binaryPath != "" || *image != "" || *local) || *local && *image == "" {
		return install.UpdateOptions{}, lifecycleArgsError()
	}
	if !*rollback && !*recover && selection.Channel == "" && *binaryPath == "" && *image == "" {
		selection = distribution.Selection{Channel: "release", Ref: "latest"}
	}
	return install.UpdateOptions{
		Selection: selection, Recover: *recover, LocalImage: *local,
		Image:      *image,
		BinaryPath: *binaryPath,
		SkipBackup: *skipBackup,
		Rollback:   *rollback,
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
	}, nil
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

	p, err := install.InstalledPlan(h, st)
	if err != nil {
		return err
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
	fmt.Printf("access:      %s (%s)\n", p.Exposure, st.TLSReadiness)
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
