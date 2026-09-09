package install

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ExposureHealthStatus is a stable, presentation-neutral access check result.
type ExposureHealthStatus string

const (
	ExposureHealthPass ExposureHealthStatus = "pass"
	ExposureHealthWarn ExposureHealthStatus = "warn"
	ExposureHealthFail ExposureHealthStatus = "fail"
	ExposureHealthSkip ExposureHealthStatus = "skip"
)

// ExposureHealthCheck contains bounded operator-safe detail. It must never
// contain certificate private keys, DNS credentials, or raw configuration.
type ExposureHealthCheck struct {
	Name   string
	Status ExposureHealthStatus
	Detail string
	Remedy string
}

// ExposureHealthReport is shared by `exposure status`, the manager and doctor.
type ExposureHealthReport struct {
	Plan                 Plan
	Identity             string
	CertificateSANs      string
	CertificateIssuer    string
	CertificateExpiresAt time.Time
	Renewal              string
	Checks               []ExposureHealthCheck
}

func (r *ExposureHealthReport) add(name string, status ExposureHealthStatus, detail, remedy string) {
	r.Checks = append(r.Checks, ExposureHealthCheck{Name: name, Status: status, Detail: detail, Remedy: remedy})
}

func (r *ExposureHealthReport) Failures() int {
	n := 0
	for _, check := range r.Checks {
		if check.Status == ExposureHealthFail {
			n++
		}
	}
	return n
}

// DiagnoseExposure performs bounded, read-only inspection of the installed
// panel access topology. The only subprocess with side effects is `nginx -t`,
// which parses configuration without reloading it.
func DiagnoseExposure(ctx context.Context, h Host, st *State, now time.Time) (*ExposureHealthReport, error) {
	if st == nil {
		return nil, fmt.Errorf("installer: WG-Guard is not installed")
	}
	p, err := installedPlan(h, st)
	if err != nil {
		return nil, err
	}
	report := &ExposureHealthReport{Plan: p}
	cfg, err := ReadBootConfig(h, st.ConfigPath)
	if err != nil {
		return nil, err
	}
	if cfg.HTTPListen != p.HTTPListen() {
		report.add("panel-listener", ExposureHealthFail, "runtime listener differs from the managed access plan", "reconfigure panel access; public plaintext is not supported")
	} else if p.Exposure == ExposureDirect {
		report.add("panel-listener", ExposureHealthPass, "public TLS listener "+cfg.HTTPListen, "")
	} else {
		report.add("panel-listener", ExposureHealthPass, "private loopback listener "+cfg.HTTPListen, "")
	}
	if p.Exposure == ExposurePrivate {
		report.add("panel-access", ExposureHealthPass, "loopback-only; use an SSH tunnel", "")
		return report, nil
	}
	if p.PublicURL() == "" {
		report.add("panel-access", ExposureHealthFail, "public HTTPS URL is not derivable", "reconfigure panel access")
	} else {
		report.add("panel-access", ExposureHealthPass, p.PublicURL(), "")
	}

	if p.Certificate == CertificateExternal {
		report.Renewal = "owned by external proxy"
		report.add("certificate", ExposureHealthWarn, "owned by the external HTTPS proxy; local trust and expiry are not verified", "verify the proxy certificate and renewal policy")
		return report, nil
	}
	if p.Certificate == CertificateBuiltin {
		report.Renewal = "built-in ACME"
		report.add("certificate", readinessStatus(st.TLSReadiness), "built-in ACME; runtime owns issuance and renewal", "run wg-guard tls-check if readiness is not verified")
		return report, nil
	}

	identity, err := certificateIdentity(st)
	if err != nil {
		report.add("certificate", ExposureHealthFail, "recorded certificate identity is invalid", "reconfigure panel access")
		return report, nil
	}
	report.Identity = identity
	leaf, certErr := inspectManagedCertificate(h, p.CertFile, p.KeyFile, identity)
	if certErr != nil {
		report.add("certificate", ExposureHealthFail, certErr.Error(), "replace or reissue the certificate through sudo wg-guard")
	} else {
		report.CertificateSANs = boundedSANs(leaf)
		report.CertificateIssuer = certificateIssuerClass(p.Certificate, leaf)
		report.CertificateExpiresAt = leaf.NotAfter
		status, detail, remedy := certificateExpiryStatus(p.Certificate, leaf, now)
		report.add("certificate", status, detail, remedy)
	}

	if managedCertificateSource(p.Certificate) {
		report.Renewal = "Certbot automatic renewal"
		checkManagedRenewal(ctx, h, st, identity, report)
	} else {
		report.Renewal = "operator-managed"
		detail := "manual certificate; WG-Guard cannot renew it"
		if p.Certificate == CertificateCloudflareOrigin {
			detail = "Cloudflare Origin CA; valid only behind Cloudflare Full (strict)"
		}
		report.add("renewal", ExposureHealthWarn, detail, "replace the certificate before expiry through sudo wg-guard")
	}
	if p.Exposure == ExposureNginx {
		checkManagedNginx(ctx, h, p, report)
	}
	return report, nil
}

func readinessStatus(value string) ExposureHealthStatus {
	if value == "verified" {
		return ExposureHealthPass
	}
	return ExposureHealthWarn
}

func inspectManagedCertificate(h Host, certFile, keyFile, identity string) (*x509.Certificate, error) {
	if certFile == "" || keyFile == "" {
		return nil, fmt.Errorf("managed certificate paths are missing")
	}
	certPEM, err := readBoundedFile(h, certFile, 1<<20)
	if err != nil {
		return nil, fmt.Errorf("certificate file is missing or unreadable")
	}
	defer clear(certPEM)
	keyPEM, err := readBoundedFile(h, keyFile, 1<<20)
	if err != nil {
		return nil, fmt.Errorf("private-key file is missing or unreadable")
	}
	defer clear(keyPEM)
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil || len(pair.Certificate) == 0 {
		return nil, fmt.Errorf("certificate and private key do not form a valid pair")
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, fmt.Errorf("certificate PEM is invalid")
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("certificate PEM is invalid")
	}
	if err := leaf.VerifyHostname(identity); err != nil {
		return nil, fmt.Errorf("certificate SAN does not match the public panel identity")
	}
	return leaf, nil
}

func boundedSANs(leaf *x509.Certificate) string {
	values := append([]string(nil), leaf.DNSNames...)
	for _, ip := range leaf.IPAddresses {
		values = append(values, ip.String())
	}
	sort.Strings(values)
	if len(values) > 8 {
		values = append(values[:8], "…")
	}
	result := strings.Join(values, ", ")
	if len(result) > 256 {
		result = result[:255] + "…"
	}
	return result
}

func certificateIssuerClass(source CertificateSource, leaf *x509.Certificate) string {
	if source == CertificateCloudflareOrigin {
		return "Cloudflare Origin CA"
	}
	if source == CertificateWebroot || source == CertificateCloudflareDNS || source == CertificateIP {
		return "public ACME CA"
	}
	if leaf.CheckSignatureFrom(leaf) == nil {
		return "private / self-signed"
	}
	issuer := strings.TrimSpace(leaf.Issuer.CommonName)
	if issuer == "" && len(leaf.Issuer.Organization) > 0 {
		issuer = strings.TrimSpace(leaf.Issuer.Organization[0])
	}
	if issuer == "" {
		return "operator-provided CA"
	}
	issuer = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, issuer)
	if len(issuer) > 96 {
		issuer = issuer[:96]
	}
	return issuer
}

func certificateExpiryStatus(source CertificateSource, leaf *x509.Certificate, now time.Time) (ExposureHealthStatus, string, string) {
	remaining := leaf.NotAfter.Sub(now)
	detail := fmt.Sprintf("SAN %s · %s · expires %s (%s)", boundedSANs(leaf), certificateIssuerClass(source, leaf), leaf.NotAfter.UTC().Format(time.RFC3339), compactDuration(remaining))
	if now.Before(leaf.NotBefore.Add(-5 * time.Minute)) {
		return ExposureHealthFail, detail + " · not yet valid", "fix system time or replace the certificate"
	}
	if remaining <= 0 {
		return ExposureHealthFail, detail + " · expired", "renew or replace the certificate immediately"
	}
	warnBefore := 30 * 24 * time.Hour
	if source == CertificateIP {
		warnBefore = 48 * time.Hour
	}
	if remaining <= warnBefore {
		return ExposureHealthWarn, detail, "check automatic renewal now"
	}
	return ExposureHealthPass, detail, ""
}

func compactDuration(d time.Duration) string {
	if d <= 0 {
		return "expired"
	}
	if d < 48*time.Hour {
		return fmt.Sprintf("%dh remaining", int(d.Hours()))
	}
	return fmt.Sprintf("%dd remaining", int(d.Hours()/24))
}

func checkManagedRenewal(ctx context.Context, h Host, st *State, identity string, report *ExposureHealthReport) {
	expectedLineage := CertificateLineage(identity)
	renewalPath := "/etc/letsencrypt/renewal/" + expectedLineage + ".conf"
	info, err := h.Stat(renewalPath)
	if st.Exposure.Lineage != expectedLineage || err != nil || !info.Mode().IsRegular() {
		report.add("renewal-lineage", ExposureHealthFail, "recorded Certbot lineage or renewal configuration is missing", "reconfigure panel access to repair certificate ownership")
	} else {
		report.add("renewal-lineage", ExposureHealthPass, expectedLineage, "")
	}
	if err := validateCertificateHook(h); err != nil {
		report.add("renewal-hook", ExposureHealthFail, "managed deploy hook is missing, changed, or has unsafe permissions", "reconfigure panel access before the next renewal")
	} else {
		report.add("renewal-hook", ExposureHealthPass, "locked certificate sync and service reload", "")
	}

	enabled, enabledErr := h.Output(ctx, []string{"systemctl", "is-enabled", "snap.certbot.renew.timer"}, 10*time.Second)
	active, activeErr := h.Output(ctx, []string{"systemctl", "is-active", "snap.certbot.renew.timer"}, 10*time.Second)
	if enabledErr != nil || activeErr != nil || strings.TrimSpace(enabled) != "enabled" || strings.TrimSpace(active) != "active" {
		report.add("renewal-timer", ExposureHealthFail, "Certbot renewal timer is not enabled and active", "enable snap.certbot.renew.timer, then run the renewal check")
	} else {
		report.add("renewal-timer", ExposureHealthPass, "enabled and active", "")
	}

	if st.Exposure.Certificate == CertificateCloudflareDNS {
		credential, err := h.Stat(CloudflareTokenPath)
		if err != nil || !credential.Mode().IsRegular() || credential.Mode().Perm() != 0o600 {
			report.add("dns-credentials", ExposureHealthFail, "Cloudflare credentials are missing or not mode 0600", "reconfigure DNS-01 with a scoped token")
		} else {
			report.add("dns-credentials", ExposureHealthPass, "root-only mode 0600", "")
		}
	}
}

func checkManagedNginx(ctx context.Context, h Host, p Plan, report *ExposureHealthReport) {
	raw, err := readBoundedFile(h, NginxConfigPath, 1<<20)
	if err != nil || string(raw) != renderNginxProxy(p) {
		clear(raw)
		report.add("nginx", ExposureHealthFail, "managed Nginx configuration is missing or has drifted", "reconfigure panel access; foreign files are never overwritten")
		return
	}
	clear(raw)
	active, activeErr := h.Output(ctx, []string{"systemctl", "is-active", "nginx.service"}, 10*time.Second)
	if activeErr != nil || strings.TrimSpace(active) != "active" {
		report.add("nginx", ExposureHealthFail, "Nginx is not active", "restore the host Nginx service, then recheck")
		return
	}
	if err := h.Run(ctx, []string{"nginx", "-q", "-t"}, 30*time.Second); err != nil {
		report.add("nginx", ExposureHealthFail, "Nginx configuration test failed", "fix Nginx without deleting foreign virtual hosts")
		return
	}
	report.add("nginx", ExposureHealthPass, "managed HTTPS proxy is active and configuration-valid", "")
}
