package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/config"
	"github.com/Sir-Adnan/wg-guard/internal/i18n"
	"github.com/Sir-Adnan/wg-guard/internal/install"
	"github.com/Sir-Adnan/wg-guard/internal/terminal"
)

func runExposure(args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	return runExposureWith(ctx, args, install.NewRealHost(), os.Stdin, os.Stdout)
}

func runExposureWith(ctx context.Context, args []string, h install.Host, in io.Reader, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: wg-guard exposure status|configure|renew|private|recover")
	}
	if args[0] == "recover" {
		if len(args) != 1 {
			return lifecycleArgsError()
		}
		return install.RecoverExposure(ctx, h)
	}
	st, err := install.LoadState(h)
	if err != nil {
		return err
	}
	if st == nil {
		return fmt.Errorf("WG-Guard is not installed")
	}
	current, err := install.InstalledPlan(h, st)
	if err != nil {
		return err
	}
	opts := terminal.Detect(in, out, i18n.En)
	opts.Context = ctx
	u := terminal.New(in, out, opts)
	switch args[0] {
	case "status":
		if len(args) != 1 {
			return lifecycleArgsError()
		}
		report, err := install.DiagnoseExposure(ctx, h, st, time.Now())
		if err != nil {
			return err
		}
		showExposureStatus(u, report, st)
		return nil
	case "renew":
		if len(args) != 1 {
			return lifecycleArgsError()
		}
		return install.RenewManagedCertificate(ctx, h)
	case "private":
		fs := flag.NewFlagSet("exposure private", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		yes := fs.Bool("yes", false, "apply without confirmation")
		if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
			return lifecycleArgsError()
		}
		if !*yes {
			ok, err := u.Confirm("Disable public panel access and require an SSH tunnel?")
			if err != nil || !ok {
				if err != nil {
					return err
				}
				return terminal.ErrCanceled
			}
		}
		candidate := current
		candidate.Exposure = install.ExposurePrivate
		candidate.Certificate = install.CertificateAuto
		candidate.Domain = ""
		candidate.TLSMode = config.TLSModeDev
		candidate.TLSModeExplicit = false
		_, err = install.ReconfigureExposure(ctx, h, install.ReconfigureOptions{Plan: candidate, Stdout: out})
		return err
	case "configure":
		candidate, yes, err := accessOptions(args[1:], h, current)
		if err != nil {
			return err
		}
		if !yes {
			candidate, err = promptAccessPlan(ctx, u, h, current)
			if err != nil {
				return err
			}
		}
		_, err = install.ReconfigureExposure(ctx, h, install.ReconfigureOptions{Plan: candidate, Stdout: out})
		candidate.CloudflareToken = ""
		return err
	default:
		return fmt.Errorf("usage: wg-guard exposure status|configure|renew|private|recover")
	}
}

func accessOptions(args []string, h install.Host, current install.Plan) (install.Plan, bool, error) {
	fs := flag.NewFlagSet("exposure configure", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	currentCertificate := string(current.Certificate)
	if currentCertificate == "" {
		currentCertificate = string(install.CertificateAuto)
	}
	currentACMEPort := current.ACMEHTTPPort
	if currentACMEPort == 0 {
		currentACMEPort = 80
	}
	exposure := fs.String("exposure", string(current.Exposure), "private|direct|nginx|external-proxy|auto")
	certificate := fs.String("certificate", currentCertificate, "auto|builtin|webroot|cloudflare-dns|ip|manual|cloudflare-origin|external")
	domain := fs.String("domain", current.Domain, "panel hostname")
	publicIP := fs.String("public-ip", current.PublicIP, "public server IP")
	panelPort := fs.Int("panel-port", current.PanelPort, "panel/backend port")
	publicPort := fs.Int("https-port", current.PublicPort, "public HTTPS port")
	acmePort := fs.Int("acme-http-port", currentACMEPort, "HTTP-01 port")
	acmeEmail := fs.String("acme-email", current.ACMEEmail, "ACME account email")
	certFile := fs.String("cert-file", current.CertFile, "certificate source file")
	keyFile := fs.String("key-file", current.KeyFile, "private-key source file")
	cloudflareFile := fs.String("cloudflare-token-file", "", "root-private Cloudflare token file")
	yes := fs.Bool("yes", false, "non-interactive")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return install.Plan{}, false, lifecycleArgsError()
	}
	validExposure := map[string]bool{"auto": true, "private": true, "direct": true, "nginx": true, "external-proxy": true}
	validCertificate := map[string]bool{"auto": true, "builtin": true, "webroot": true, "cloudflare-dns": true, "ip": true, "manual": true, "cloudflare-origin": true, "external": true}
	if !validExposure[*exposure] || !validCertificate[*certificate] || *panelPort < 1 || *panelPort > 65535 || *publicPort < 0 || *publicPort > 65535 || *acmePort < 1 || *acmePort > 65535 {
		return install.Plan{}, false, lifecycleArgsError()
	}
	visited := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { visited[f.Name] = true })
	p := current
	p.Exposure = install.ExposureMode(*exposure)
	p.Certificate = install.CertificateSource(*certificate)
	p.Domain = *domain
	p.PublicIP = *publicIP
	p.PanelPort = *panelPort
	p.PanelPortExplicit = visited["panel-port"]
	p.PublicPort = *publicPort
	p.ACMEHTTPPort = *acmePort
	p.ACMEEmail = *acmeEmail
	p.CertFile = *certFile
	p.KeyFile = *keyFile
	p.TLSMode = config.TLSModeDev
	p.TLSModeExplicit = false
	if visited["domain"] && !visited["exposure"] {
		p.Exposure = install.ExposureAuto
	}
	if visited["domain"] && !visited["certificate"] {
		p.Certificate = install.CertificateAuto
	}
	if *cloudflareFile != "" {
		if p.Certificate != install.CertificateCloudflareDNS {
			return install.Plan{}, false, fmt.Errorf("--cloudflare-token-file requires --certificate cloudflare-dns")
		}
		var err error
		p.CloudflareToken, err = install.ReadCloudflareToken(h, *cloudflareFile)
		if err != nil {
			return install.Plan{}, false, err
		}
	}
	return p, *yes, nil
}

func promptAccessPlan(ctx context.Context, u *terminal.UI, h install.Host, current install.Plan) (install.Plan, error) {
	u.Section("Panel access & HTTPS")
	u.Text("Choose one clear access model. Enter keeps the safest recommended choice.")
	choice, err := u.Choose("Access model", []string{
		"Keep current settings (recommended)",
		"Secure HTTPS with a domain",
		"Secure HTTPS on this server IP",
		"Existing external reverse proxy",
		"Private access through an SSH tunnel",
	}, 1)
	if err != nil {
		return install.Plan{}, err
	}
	if choice == 1 {
		return current, nil
	}
	p := current
	p.TLSMode = config.TLSModeDev
	p.TLSModeExplicit = false
	p.CloudflareToken = ""
	p.ACMEEmail = ""
	switch choice {
	case 2:
		p.Domain, err = u.Ask("Panel domain", current.Domain)
		if err != nil {
			return p, err
		}
		method, err := u.Choose("Certificate & proxy", []string{
			"Automatic safe setup (recommended)",
			"Cloudflare DNS-01 · no validation port",
			"Manual browser-trusted certificate files",
			"Cloudflare Origin CA · proxied hostname only",
			"Certificate is owned by an existing proxy",
		}, 1)
		if err != nil {
			return p, err
		}
		switch method {
		case 1:
			p.Exposure, p.Certificate = install.ExposureAuto, install.CertificateAuto
		case 2:
			p.Exposure, p.Certificate = install.ExposureAuto, install.CertificateCloudflareDNS
			p.CloudflareToken, err = u.Secret("Scoped Cloudflare API token (hidden)")
		case 3:
			p.Exposure, p.Certificate = install.ExposureAuto, install.CertificateManual
			p.CertFile, err = u.Ask("Certificate file", "")
			if err == nil {
				p.KeyFile, err = u.Ask("Private-key file", "")
			}
		case 4:
			p.Exposure, p.Certificate = install.ExposureNginx, install.CertificateCloudflareOrigin
			p.CertFile, err = u.Ask("Origin certificate file", "")
			if err == nil {
				p.KeyFile, err = u.Ask("Origin private-key file", "")
			}
		case 5:
			p.Exposure, p.Certificate = install.ExposureExternalProxy, install.CertificateExternal
		}
		if err != nil {
			return p, err
		}
		if method == 1 || method == 2 {
			p.ACMEEmail, err = u.Ask("ACME email (optional)", "")
			if err != nil {
				return p, err
			}
		}
	case 3:
		p.Exposure, p.Certificate = install.ExposureDirect, install.CertificateIP
		p.Domain = ""
		p.PublicIP, err = u.Ask("Public server IP", current.PublicIP)
		if err != nil {
			return p, err
		}
		p.ACMEEmail, err = u.Ask("ACME email (optional)", "")
		if err != nil {
			return p, err
		}
		u.Text("IP certificates are short-lived and require public TCP 80 for automatic renewal.")
	case 4:
		p.Exposure, p.Certificate = install.ExposureExternalProxy, install.CertificateExternal
		p.Domain, err = u.Ask("Public proxy hostname", current.Domain)
		if err != nil {
			return p, err
		}
	case 5:
		p.Exposure, p.Certificate = install.ExposurePrivate, install.CertificateAuto
		p.Domain = ""
	}
	advanced, err := u.ConfirmDefault("Customize ports?", false)
	if err != nil {
		return p, err
	}
	if advanced {
		p.PanelPort, err = askAccessPort(u, "Panel / loopback backend port", p.PanelPort)
		if err != nil {
			return p, err
		}
		p.PanelPortExplicit = true
		if p.Exposure != install.ExposurePrivate {
			p.PublicPort, err = askAccessPort(u, "Public HTTPS port", firstPositive(p.PublicPort, 443))
			if err != nil {
				return p, err
			}
		}
	}
	resolved, err := install.ResolveReconfigurePlan(ctx, h, current, p)
	if err != nil {
		return p, err
	}
	u.Section("Review")
	u.Field("Access", string(resolved.Exposure))
	u.Field("Certificate", firstNonemptyString(string(resolved.Certificate), "none"))
	u.Field("Panel listener", resolved.HTTPListen())
	u.Field("Public URL", firstNonemptyString(resolved.PublicURL(), "private · SSH tunnel only"))
	confirmed, err := u.ConfirmDefault("Apply this reversible access change?", true)
	if err != nil {
		return p, err
	}
	if !confirmed {
		return p, terminal.ErrCanceled
	}
	return resolved, nil
}

func askAccessPort(u *terminal.UI, label string, def int) (int, error) {
	for {
		value, err := u.Ask(label, strconv.Itoa(def))
		if err != nil {
			return 0, err
		}
		port, err := strconv.Atoi(terminal.Digits(value))
		if err == nil && port >= 1 && port <= 65535 {
			return port, nil
		}
		u.Text("Enter a TCP port between 1 and 65535.")
	}
}

func showExposureStatus(u *terminal.UI, report *install.ExposureHealthReport, st *install.State) {
	p := report.Plan
	public := p.PublicURL()
	if public == "" {
		public = "Private · use: " + p.SSHTunnel()
	}
	fields := []terminal.StatusField{
		{Label: "Access", Value: string(p.Exposure)},
		{Label: "Panel", Value: public},
		{Label: "Listener", Value: p.HTTPListen()},
		{Label: "Certificate", Value: firstNonemptyString(string(p.Certificate), "none")},
		{Label: "Readiness", Value: firstNonemptyString(st.TLSReadiness, "unknown")},
	}
	if st.Exposure.Lineage != "" {
		fields = append(fields, terminal.StatusField{Label: "Renewal", Value: report.Renewal + " · " + st.Exposure.Lineage})
	} else if report.Renewal != "" {
		fields = append(fields, terminal.StatusField{Label: "Renewal", Value: report.Renewal})
	}
	if !report.CertificateExpiresAt.IsZero() {
		fields = append(fields,
			terminal.StatusField{Label: "Issuer", Value: report.CertificateIssuer},
			terminal.StatusField{Label: "Expires", Value: report.CertificateExpiresAt.UTC().Format(time.RFC3339)},
		)
	}
	u.StatusCard("Panel access", "Current configuration", fields)
	for _, check := range report.Checks {
		if check.Status != install.ExposureHealthWarn && check.Status != install.ExposureHealthFail {
			continue
		}
		label := strings.ToUpper(string(check.Status)) + " · " + check.Name
		u.Field(label, check.Detail)
		if check.Remedy != "" {
			u.Text("Next: " + check.Remedy)
		}
	}
}

func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func firstNonemptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
