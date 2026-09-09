package install

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strconv"
	"strings"
	"time"
)

const nginxManagedMarker = "# Managed by WG-Guard — use `sudo wg-guard` to change panel access."

func validateNginxPlan(p Plan) error {
	if p.Exposure != ExposureNginx || !validHostname(p.Domain) || p.PanelPort < 1 || p.PanelPort > 65535 || p.PublicPort < 1 || p.PublicPort > 65535 {
		return fmt.Errorf("installer: invalid managed Nginx plan")
	}
	switch p.Certificate {
	case CertificateWebroot, CertificateCloudflareDNS, CertificateManual, CertificateCloudflareOrigin:
		return nil
	default:
		return fmt.Errorf("installer: certificate source %q is incompatible with managed Nginx", p.Certificate)
	}
}

func renderNginxChallenge(p Plan) string {
	return nginxManagedMarker + `
server {
    listen 80;
    server_name ` + p.Domain + `;

    location ^~ /.well-known/acme-challenge/ {
        default_type text/plain;
        root ` + ACMEWebrootPath + `;
        try_files $uri =404;
    }

    location / {
        return 404;
    }
}
`
}

func renderNginxProxy(p Plan) string {
	publicPort := strconv.Itoa(p.PublicPort)
	publicAuthority := p.Domain
	if p.PublicPort != 443 {
		publicAuthority += ":" + publicPort
	}
	return nginxManagedMarker + `
server {
    listen 80;
    server_name ` + p.Domain + `;

    location ^~ /.well-known/acme-challenge/ {
        default_type text/plain;
        root ` + ACMEWebrootPath + `;
        try_files $uri =404;
    }

    location / {
        return 308 https://` + publicAuthority + `$request_uri;
    }
}

server {
    listen ` + publicPort + ` ssl;
    server_name ` + p.Domain + `;
    server_tokens off;

    ssl_certificate ` + ManagedCertPath + `;
    ssl_certificate_key ` + ManagedKeyPath + `;
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_session_cache shared:WGGuardTLS:10m;
    ssl_session_timeout 1d;
    ssl_session_tickets off;

    add_header Strict-Transport-Security "max-age=31536000" always;
    add_header X-Content-Type-Options "nosniff" always;
    add_header X-Frame-Options "DENY" always;
    add_header Referrer-Policy "same-origin" always;
    add_header Permissions-Policy "camera=(), microphone=(), geolocation=()" always;

    location / {
        proxy_http_version 1.1;
        proxy_set_header Host ` + p.Domain + `;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $remote_addr;
        proxy_set_header X-Forwarded-Proto https;
        proxy_set_header X-Forwarded-Host ` + p.Domain + `;
        proxy_set_header X-Forwarded-Port ` + publicPort + `;
        proxy_set_header Connection "";
        proxy_read_timeout 60s;
        proxy_send_timeout 60s;
        proxy_pass http://127.0.0.1:` + strconv.Itoa(p.PanelPort) + `;
    }
}
`
}

// PrepareNginx creates only a challenge route. It never proxies plaintext to
// the panel. FinalizeNginx is called only after certificate validation.
func PrepareNginx(ctx context.Context, h Host, p Plan, st *State, out io.Writer) (func() error, error) {
	if err := validateNginxPlan(p); err != nil {
		return nil, err
	}
	for _, path := range []string{NginxConfigPath, ACMEWebrootPath} {
		if _, err := h.Stat(path); err == nil {
			return nil, fmt.Errorf("installer: %s already exists without WG-Guard install ownership", path)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	facts, err := InspectExposure(ctx, h, p.Domain)
	if err != nil {
		return nil, err
	}
	if !facts.NginxInstalled || !facts.NginxActive || !facts.NginxStandard || facts.NginxDomainConflict {
		return nil, fmt.Errorf("installer: managed Nginx needs an active standard Ubuntu configuration and an unused hostname")
	}
	if err := h.MkdirAll(ACMEWebrootPath, 0o755); err != nil {
		return nil, err
	}
	before := fileSnapshot{}
	if err := replaceNginxConfig(ctx, h, []byte(renderNginxChallenge(p)), before, true); err != nil {
		_ = h.RemoveAll(ACMEWebrootPath)
		return nil, err
	}
	if st != nil {
		st.Exposure.NginxConfigPath = NginxConfigPath
		st.Exposure.ACMEWebroot = ACMEWebrootPath
	}
	if out != nil {
		progress(out, "nginx_challenge", p.Domain)
	}
	cleanup := func() error {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		if err := restoreNginxConfig(rollbackCtx, h, before, true); err != nil {
			return err
		}
		return h.RemoveAll(ACMEWebrootPath)
	}
	return cleanup, nil
}

// FinalizeNginx atomically promotes the active challenge-only server to the
// TLS reverse proxy. A failed validation or reload restores the challenge.
func FinalizeNginx(ctx context.Context, h Host, p Plan, out io.Writer) error {
	if err := validateNginxPlan(p); err != nil {
		return err
	}
	before, err := captureFile(h, NginxConfigPath, 1<<20)
	if err != nil {
		return err
	}
	if !before.exists || string(before.data) != renderNginxChallenge(p) {
		return fmt.Errorf("installer: managed Nginx challenge configuration changed unexpectedly")
	}
	if err := replaceNginxConfig(ctx, h, []byte(renderNginxProxy(p)), before, true); err != nil {
		return err
	}
	if out != nil {
		progress(out, "nginx", p.PublicURL())
	}
	return nil
}

func replaceNginxConfig(ctx context.Context, h Host, candidate []byte, before fileSnapshot, active bool) error {
	if err := atomicWrite(h, NginxConfigPath, candidate, 0o644); err != nil {
		return err
	}
	if err := validateReloadNginx(ctx, h, active); err != nil {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		return errors.Join(err, restoreNginxConfig(rollbackCtx, h, before, active))
	}
	return nil
}

func validateReloadNginx(ctx context.Context, h Host, active bool) error {
	if err := h.Run(ctx, []string{"nginx", "-t"}, 30*time.Second); err != nil {
		return fmt.Errorf("installer: Nginx configuration check failed: %w", err)
	}
	if active {
		if err := h.Run(ctx, []string{"systemctl", "reload", "nginx.service"}, 30*time.Second); err != nil {
			return fmt.Errorf("installer: Nginx reload failed: %w", err)
		}
	}
	return nil
}

func restoreNginxConfig(ctx context.Context, h Host, before fileSnapshot, active bool) error {
	var restoreErr error
	if before.exists {
		restoreErr = atomicWrite(h, NginxConfigPath, before.data, before.mode)
	} else if err := h.Remove(NginxConfigPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		restoreErr = err
	}
	if restoreErr != nil {
		return fmt.Errorf("installer: restore Nginx configuration: %w", restoreErr)
	}
	return validateReloadNginx(ctx, h, active)
}

// RemoveManagedNginx removes only a state-recorded WG-Guard configuration.
// If validation or reload fails, the exact previous file is restored.
func RemoveManagedNginx(ctx context.Context, h Host, exposure ExposureState) error {
	if exposure.NginxConfigPath == "" {
		return nil
	}
	if exposure.NginxConfigPath != NginxConfigPath || exposure.ACMEWebroot != ACMEWebrootPath {
		return fmt.Errorf("installer: unsafe managed Nginx state")
	}
	before, err := captureFile(h, NginxConfigPath, 1<<20)
	if err != nil {
		return err
	}
	if !before.exists {
		return h.RemoveAll(ACMEWebrootPath)
	}
	if !strings.HasPrefix(string(before.data), nginxManagedMarker+"\n") {
		return fmt.Errorf("installer: %s is no longer WG-Guard-managed; leaving it unchanged", NginxConfigPath)
	}
	active := false
	if status, statusErr := h.Output(ctx, []string{"systemctl", "is-active", "nginx.service"}, 10*time.Second); statusErr == nil {
		active = strings.TrimSpace(status) == "active"
	}
	if err := h.Remove(NginxConfigPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := validateReloadNginx(ctx, h, active); err != nil {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		return errors.Join(err, restoreNginxConfig(rollbackCtx, h, before, active))
	}
	if err := h.RemoveAll(ACMEWebrootPath); err != nil {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		return errors.Join(err, restoreNginxConfig(rollbackCtx, h, before, active))
	}
	return nil
}
