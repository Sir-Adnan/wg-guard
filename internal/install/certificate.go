package install

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const CertbotPath = "/snap/bin/certbot"

var certbotVersion = regexp.MustCompile(`(?i)certbot\s+([0-9]+)\.([0-9]+)(?:\.[0-9]+)?`)

type CertificateResult struct {
	CertFile   string
	KeyFile    string
	Lineage    string
	Identifier string
	Managed    bool
}

func CertificateLineage(identifier string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(identifier))))
	return "wg-guard-" + hex.EncodeToString(sum[:6])
}

func CertbotLivePath(lineage string) string { return "/etc/letsencrypt/live/" + lineage }

// PrepareCertificate issues or imports a certificate into WG-Guard-owned
// copies. The returned cleanup restores the exact prior copies if a later
// install step fails. Secrets are written only to a 0600 file, never argv.
func PrepareCertificate(ctx context.Context, h Host, p Plan, st *State, out io.Writer) (CertificateResult, func(), error) {
	identifier := p.Domain
	if identifier == "" {
		identifier = p.PublicIP
	}
	switch p.Certificate {
	case "", CertificateBuiltin, CertificateExternal:
		return CertificateResult{Identifier: identifier}, func() {}, nil
	}
	if identifier == "" {
		return CertificateResult{}, nil, fmt.Errorf("installer: certificate identity is missing")
	}
	certBefore, err := captureFile(h, ManagedCertPath, 1<<20)
	if err != nil {
		return CertificateResult{}, nil, fmt.Errorf("installer: snapshot managed certificate: %w", err)
	}
	keyBefore, err := captureFile(h, ManagedKeyPath, 1<<20)
	if err != nil {
		return CertificateResult{}, nil, fmt.Errorf("installer: snapshot managed private key: %w", err)
	}
	credentialsBefore, err := captureFile(h, CloudflareTokenPath, 4096)
	if err != nil {
		return CertificateResult{}, nil, fmt.Errorf("installer: snapshot Cloudflare credentials: %w", err)
	}
	if err := h.MkdirAll(ManagedTLSDir, 0o700); err != nil {
		return CertificateResult{}, nil, err
	}
	cleanup := func() {
		restoreFile(h, ManagedCertPath, certBefore, 0o644)
		restoreFile(h, ManagedKeyPath, keyBefore, 0o600)
		restoreFile(h, CloudflareTokenPath, credentialsBefore, 0o600)
	}
	fail := func(err error) (CertificateResult, func(), error) {
		cleanup()
		return CertificateResult{}, nil, err
	}

	lineage := ""
	sourceCert, sourceKey := p.CertFile, p.KeyFile
	switch p.Certificate {
	case CertificateManual, CertificateCloudflareOrigin:
		// The source files remain operator-owned; WG-Guard owns stable copies.
	case CertificateWebroot, CertificateCloudflareDNS, CertificateIP:
		if err := ensureCertbot(ctx, h, p.Certificate == CertificateCloudflareDNS); err != nil {
			return fail(err)
		}
		lineage = CertificateLineage(identifier)
		if p.Certificate == CertificateCloudflareDNS {
			if !cloudflareAPIToken.MatchString(p.CloudflareToken) {
				return fail(fmt.Errorf("installer: Cloudflare DNS-01 needs a valid scoped API token"))
			}
			credentials := []byte("dns_cloudflare_api_token = " + p.CloudflareToken + "\n")
			if err := atomicWrite(h, CloudflareTokenPath, credentials, 0o600); err != nil {
				clear(credentials)
				return fail(err)
			}
			clear(credentials)
		}
		args := certbotIssueArgs(p, identifier, lineage)
		if err := h.Run(ctx, args, longTimeout); err != nil {
			return fail(fmt.Errorf("installer: automatic certificate issuance failed: %w", err))
		}
		sourceCert = CertbotLivePath(lineage) + "/fullchain.pem"
		sourceKey = CertbotLivePath(lineage) + "/privkey.pem"
	default:
		return fail(fmt.Errorf("installer: unsupported certificate source %q", p.Certificate))
	}

	certPEM, err := readBoundedFile(h, sourceCert, 1<<20)
	if err != nil {
		return fail(fmt.Errorf("installer: read issued certificate: %w", err))
	}
	keyPEM, err := readBoundedFile(h, sourceKey, 1<<20)
	if err != nil {
		clear(certPEM)
		return fail(fmt.Errorf("installer: read issued private key: %w", err))
	}
	if err := validateCertificateMaterial(certPEM, keyPEM, identifier, time.Now()); err != nil {
		clear(certPEM)
		clear(keyPEM)
		return fail(err)
	}
	if err := atomicWrite(h, ManagedCertPath, certPEM, 0o644); err != nil {
		clear(certPEM)
		clear(keyPEM)
		return fail(err)
	}
	if err := atomicWrite(h, ManagedKeyPath, keyPEM, 0o600); err != nil {
		clear(certPEM)
		clear(keyPEM)
		return fail(err)
	}
	clear(certPEM)
	clear(keyPEM)
	if st != nil {
		st.Exposure.CertFile = ManagedCertPath
		st.Exposure.KeyFile = ManagedKeyPath
		st.Exposure.Lineage = lineage
		if p.Certificate == CertificateCloudflareDNS {
			st.Exposure.CredentialsFile = CloudflareTokenPath
		}
	}
	if out != nil {
		progress(out, "certificate", identifier)
	}
	return CertificateResult{CertFile: ManagedCertPath, KeyFile: ManagedKeyPath, Lineage: lineage, Identifier: identifier, Managed: true}, cleanup, nil
}

func ensureCertbot(ctx context.Context, h Host, cloudflare bool) error {
	version, err := h.Output(ctx, []string{CertbotPath, "--version"}, 15*time.Second)
	if err != nil || !certbotAtLeast(version, 5, 4) {
		if _, lookErr := h.LookPath("snap"); lookErr != nil {
			return fmt.Errorf("installer: Certbot 5.4+ is required and snap is unavailable")
		}
		var commands [][]string
		if !snapPackageInstalled(ctx, h, "core") {
			commands = append(commands, []string{"snap", "install", "core"})
		}
		commands = append(commands, []string{"snap", "refresh", "core"})
		if snapPackageInstalled(ctx, h, "certbot") {
			commands = append(commands, []string{"snap", "refresh", "certbot"})
		} else {
			commands = append(commands, []string{"snap", "install", "--classic", "certbot"})
		}
		for _, args := range commands {
			if runErr := h.Run(ctx, args, longTimeout); runErr != nil {
				return fmt.Errorf("installer: prepare official Certbot snap: %w", runErr)
			}
		}
		version, err = h.Output(ctx, []string{CertbotPath, "--version"}, 15*time.Second)
		if err != nil || !certbotAtLeast(version, 5, 4) {
			return fmt.Errorf("installer: Certbot 5.4 or newer is required")
		}
	}
	if cloudflare {
		if !snapPackageInstalled(ctx, h, "certbot-dns-cloudflare") {
			if err := h.Run(ctx, []string{"snap", "set", "certbot", "trust-plugin-with-root=ok"}, 30*time.Second); err != nil {
				return fmt.Errorf("installer: authorize Certbot DNS plugin: %w", err)
			}
			if err := h.Run(ctx, []string{"snap", "install", "certbot-dns-cloudflare"}, longTimeout); err != nil {
				return fmt.Errorf("installer: install Certbot Cloudflare plugin: %w", err)
			}
		}
	}
	return nil
}

func snapPackageInstalled(ctx context.Context, h Host, name string) bool {
	listed, err := h.Output(ctx, []string{"snap", "list", name}, 15*time.Second)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(listed, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == name {
			return true
		}
	}
	return false
}

func certbotAtLeast(output string, major, minor int) bool {
	match := certbotVersion.FindStringSubmatch(strings.TrimSpace(output))
	if len(match) != 3 {
		return false
	}
	gotMajor, _ := strconv.Atoi(match[1])
	gotMinor, _ := strconv.Atoi(match[2])
	return gotMajor > major || gotMajor == major && gotMinor >= minor
}

func certbotIssueArgs(p Plan, identifier, lineage string) []string {
	args := []string{CertbotPath, "certonly", "--non-interactive", "--agree-tos", "--keep-until-expiring", "--cert-name", lineage}
	if p.ACMEEmail == "" {
		args = append(args, "--register-unsafely-without-email")
	} else {
		args = append(args, "--email", p.ACMEEmail)
	}
	switch p.Certificate {
	case CertificateWebroot:
		args = append(args, "--webroot", "--webroot-path", ACMEWebrootPath, "-d", identifier)
	case CertificateCloudflareDNS:
		args = append(args, "--dns-cloudflare", "--dns-cloudflare-credentials", CloudflareTokenPath, "--dns-cloudflare-propagation-seconds", "30", "-d", identifier)
	case CertificateIP:
		args = append(args, "--preferred-profile", "shortlived", "--ip-address", identifier, "--standalone", "--http-01-port", strconv.Itoa(p.ACMEHTTPPort))
	}
	return args
}

func validateCertificateMaterial(certPEM, keyPEM []byte, identifier string, now time.Time) error {
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil || len(pair.Certificate) == 0 {
		return fmt.Errorf("installer: certificate and private key are not a matching PEM pair")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil || leaf.VerifyHostname(identifier) != nil {
		return fmt.Errorf("installer: certificate does not contain the requested domain or IP")
	}
	if now.Before(leaf.NotBefore.Add(-5*time.Minute)) || !leaf.NotAfter.After(now.Add(time.Hour)) {
		return fmt.Errorf("installer: certificate is not currently usable or expires too soon")
	}
	return nil
}

func readBoundedFile(h Host, filename string, limit int64) ([]byte, error) {
	f, err := h.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		clear(raw)
		return nil, fmt.Errorf("file exceeds %d bytes", limit)
	}
	return raw, nil
}

type fileSnapshot struct {
	exists bool
	data   []byte
	mode   fs.FileMode
}

func captureFile(h Host, filename string, limit int64) (fileSnapshot, error) {
	info, err := h.Stat(filename)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fileSnapshot{}, nil
		}
		return fileSnapshot{}, err
	}
	if !info.Mode().IsRegular() {
		return fileSnapshot{}, fmt.Errorf("existing path is not a regular file")
	}
	data, err := readBoundedFile(h, filename, limit)
	if err != nil {
		return fileSnapshot{}, err
	}
	return fileSnapshot{exists: true, data: data, mode: info.Mode().Perm()}, nil
}

func restoreFile(h Host, filename string, snapshot fileSnapshot, fallback fs.FileMode) {
	if !snapshot.exists {
		_ = h.Remove(filename)
		return
	}
	mode := snapshot.mode
	if mode == 0 {
		mode = fallback
	}
	_ = atomicWrite(h, filename, snapshot.data, mode)
}
