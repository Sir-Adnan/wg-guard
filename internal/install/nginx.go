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

func renderNginxTransition(candidate, previous Plan) string {
	current := renderNginxProxy(previous)
	if candidate.Domain == previous.Domain {
		// The live proxy already exposes the dedicated ACME webroot.
		return current
	}
	return current + "\n" + renderNginxChallenge(candidate)
}

// PrepareNginx creates only a challenge route. It never proxies plaintext to
// the panel. FinalizeNginx is called only after certificate validation.
func PrepareNginx(ctx context.Context, h Host, p Plan, st *State, out io.Writer) (func() error, error) {
	return prepareNginx(ctx, h, p, nil, st, out)
}

// PrepareNginxReplacement replaces a state-owned proxy during a locked access
// transition and returns a callback that restores its exact previous bytes.
func PrepareNginxReplacement(ctx context.Context, h Host, p, previous Plan, st *State, out io.Writer) (func() error, error) {
	if err := validateNginxPlan(previous); err != nil {
		return nil, fmt.Errorf("installer: invalid prior managed Nginx plan: %w", err)
	}
	return prepareNginx(ctx, h, p, &previous, st, out)
}

func prepareNginx(ctx context.Context, h Host, p Plan, previous *Plan, st *State, out io.Writer) (func() error, error) {
	if err := validateNginxPlan(p); err != nil {
		return nil, err
	}
	before := fileSnapshot{}
	webrootCreated := false
	if previous == nil {
		for _, path := range []string{NginxConfigPath, ACMEWebrootPath} {
			if _, err := h.Stat(path); err == nil {
				return nil, fmt.Errorf("installer: %s already exists without WG-Guard install ownership", path)
			} else if !errors.Is(err, fs.ErrNotExist) {
				return nil, err
			}
		}
		webrootCreated = true
	} else {
		var err error
		before, err = captureFile(h, NginxConfigPath, 1<<20)
		if err != nil {
			return nil, err
		}
		if !before.exists || !strings.HasPrefix(string(before.data), nginxManagedMarker+"\n") {
			return nil, fmt.Errorf("installer: prior Nginx configuration is no longer WG-Guard-managed")
		}
		info, statErr := h.Stat(ACMEWebrootPath)
		switch {
		case errors.Is(statErr, fs.ErrNotExist):
			webrootCreated = true
		case statErr != nil:
			return nil, statErr
		case !info.IsDir():
			return nil, fmt.Errorf("installer: managed ACME webroot is not a directory")
		}
	}
	facts, err := InspectExposure(ctx, h, p.Domain)
	if err != nil {
		return nil, err
	}
	ownedConflict := previous != nil && previous.Domain == p.Domain
	if !facts.NginxInstalled || !facts.NginxActive || !facts.NginxStandard || facts.NginxDomainConflict && !ownedConflict {
		return nil, fmt.Errorf("installer: managed Nginx needs an active standard Ubuntu configuration and an unused hostname")
	}
	if err := h.MkdirAll(ACMEWebrootPath, 0o755); err != nil {
		return nil, err
	}
	challenge := renderNginxChallenge(p)
	if previous != nil {
		challenge = renderNginxTransition(p, *previous)
	}
	if err := replaceNginxConfig(ctx, h, []byte(challenge), before, true); err != nil {
		if webrootCreated {
			_ = h.RemoveAll(ACMEWebrootPath)
		}
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
		if webrootCreated {
			return h.RemoveAll(ACMEWebrootPath)
		}
		return nil
	}
	return cleanup, nil
}

// FinalizeNginxReplacement promotes a no-downtime transition configuration
// after the candidate certificate is ready.
func FinalizeNginxReplacement(ctx context.Context, h Host, p, previous Plan, out io.Writer) error {
	if err := validateNginxPlan(p); err != nil {
		return err
	}
	before, err := captureFile(h, NginxConfigPath, 1<<20)
	if err != nil {
		return err
	}
	if !before.exists || string(before.data) != renderNginxTransition(p, previous) {
		return fmt.Errorf("installer: managed Nginx transition configuration changed unexpectedly")
	}
	if err := replaceNginxConfig(ctx, h, []byte(renderNginxProxy(p)), before, true); err != nil {
		return err
	}
	if out != nil {
		progress(out, "nginx", p.PublicURL())
	}
	return nil
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

// DetachManagedNginx removes the state-owned server from the active Nginx
// configuration without deleting its webroot. The callback restores it.
func DetachManagedNginx(ctx context.Context, h Host, exposure ExposureState) (func() error, error) {
	if exposure.NginxConfigPath != NginxConfigPath || exposure.ACMEWebroot != ACMEWebrootPath {
		return nil, fmt.Errorf("installer: unsafe managed Nginx state")
	}
	before, err := captureFile(h, NginxConfigPath, 1<<20)
	if err != nil {
		return nil, err
	}
	if !before.exists || !strings.HasPrefix(string(before.data), nginxManagedMarker+"\n") {
		return nil, fmt.Errorf("installer: %s is no longer WG-Guard-managed; leaving it unchanged", NginxConfigPath)
	}
	active := false
	if status, statusErr := h.Output(ctx, []string{"systemctl", "is-active", "nginx.service"}, 10*time.Second); statusErr == nil {
		active = strings.TrimSpace(status) == "active"
	}
	if err := h.Remove(NginxConfigPath); err != nil {
		return nil, err
	}
	if err := validateReloadNginx(ctx, h, active); err != nil {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		return nil, errors.Join(err, restoreNginxConfig(rollbackCtx, h, before, active))
	}
	cleanup := func() error {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		return restoreNginxConfig(rollbackCtx, h, before, active)
	}
	return cleanup, nil
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
