package install

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/config"
)

var proveExposureCertificate = WaitCertificate

// installedPlan reconstructs non-secret runtime intent from the authoritative
// state and boot configuration. Legacy state is inferred conservatively.
func installedPlan(h Host, st *State) (Plan, error) {
	cfg, err := ReadBootConfig(h, st.ConfigPath)
	if err != nil {
		return Plan{}, err
	}
	p := Defaults()
	p.Mode = st.Mode
	p.Image = st.Image
	p.DataDir = st.DataDir
	p.PublicIP = st.PublicIP
	p.TLSMode = cfg.TLS.Mode
	p.Domain = cfg.TLS.Domain
	p.PanelPort = portOf(cfg.HTTPListen)
	p.ACMEHTTPPort = cfg.TLS.ACMEHTTPPort
	p.CertFile = cfg.TLS.CertFile
	p.KeyFile = cfg.TLS.KeyFile
	p.Exposure = st.Exposure.Mode
	p.Certificate = st.Exposure.Certificate
	p.PublicPort = st.Exposure.PublicPort
	if !p.Exposure.Valid() {
		switch cfg.TLS.Mode {
		case config.TLSModeACME:
			p.Exposure, p.Certificate = ExposureDirect, CertificateBuiltin
		case config.TLSModeManual:
			p.Exposure, p.Certificate = ExposureDirect, CertificateManual
		case config.TLSModeProxy:
			if p.Domain != "" {
				p.Exposure, p.Certificate = ExposureExternalProxy, CertificateExternal
			} else {
				p.Exposure = ExposurePrivate
			}
		default:
			p.Exposure = ExposurePrivate
		}
	}
	if p.PublicPort == 0 && p.Exposure != ExposurePrivate {
		p.PublicPort = p.PanelPort
		if p.Exposure == ExposureNginx || p.Exposure == ExposureExternalProxy {
			p.PublicPort = 443
		}
	}
	return p, nil
}

func certificateIdentity(st *State) (string, error) {
	if st.Exposure.PublicURL != "" {
		u, err := url.Parse(st.Exposure.PublicURL)
		if err != nil || u.Hostname() == "" {
			return "", fmt.Errorf("installer: invalid recorded certificate identity")
		}
		return u.Hostname(), nil
	}
	if st.PublicIP != "" {
		return st.PublicIP, nil
	}
	return "", fmt.Errorf("installer: recorded certificate identity is missing")
}

// SyncManagedCertificate is the host-only Certbot deploy target. Unrelated
// lineages are intentional no-ops so one global hook can coexist safely.
func SyncManagedCertificate(ctx context.Context, h Host, renewedLineage string) error {
	if !h.IsRoot() {
		return terminalError("manage.root")
	}
	st, err := LoadState(h)
	if err != nil {
		return err
	}
	if st == nil {
		return terminalError("install.error.health.3")
	}
	switch st.Exposure.Certificate {
	case CertificateWebroot, CertificateCloudflareDNS, CertificateIP:
	default:
		return nil
	}
	expected := CertbotLivePath(st.Exposure.Lineage)
	if renewedLineage != expected {
		return nil
	}
	unlock, err := h.LockLifecycle()
	if err != nil {
		return err
	}
	defer unlock()

	journal, err := LoadJournal(h)
	if err != nil {
		return err
	}
	if journal != nil && !journal.terminal() && journal.Operation != "certificate" {
		return pendingOperationError(journal)
	}
	beforeState := *st
	if journal == nil || journal.terminal() {
		afterState := *st
		journal = &Journal{Schema: 1, ID: transactionID(), Operation: "certificate", Before: &beforeState, After: &afterState}
	}
	identity, err := certificateIdentity(st)
	if err != nil {
		return err
	}
	certPEM, err := readBoundedFile(h, expected+"/fullchain.pem", 1<<20)
	if err != nil {
		return err
	}
	defer clear(certPEM)
	keyPEM, err := readBoundedFile(h, expected+"/privkey.pem", 1<<20)
	if err != nil {
		return err
	}
	defer clear(keyPEM)
	if err := validateCertificateMaterial(certPEM, keyPEM, identity, time.Now()); err != nil {
		return err
	}
	certBefore, err := captureFile(h, ManagedCertPath, 1<<20)
	if err != nil {
		return err
	}
	keyBefore, err := captureFile(h, ManagedKeyPath, 1<<20)
	if err != nil {
		return err
	}
	if err := journal.save(h, "prepared"); err != nil {
		return err
	}
	activate := func(activateCtx context.Context) error {
		if st.Exposure.Mode == ExposureNginx {
			return validateReloadNginx(activateCtx, h, true)
		}
		return restartLocked(activateCtx, h, st, nil)
	}
	rollback := func(cause error) error {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		restoreErr := errors.Join(
			restoreCapturedFile(h, ManagedKeyPath, keyBefore),
			restoreCapturedFile(h, ManagedCertPath, certBefore),
		)
		if restoreErr == nil {
			restoreErr = activate(rollbackCtx)
		}
		return errors.Join(cause, restoreErr, journal.save(h, "rolled-back"))
	}
	if err := atomicWrite(h, ManagedKeyPath, keyPEM, 0o600); err != nil {
		return rollback(err)
	}
	if err := atomicWrite(h, ManagedCertPath, certPEM, 0o644); err != nil {
		return rollback(err)
	}
	if err := journal.save(h, "started"); err != nil {
		return rollback(err)
	}
	if err := activate(ctx); err != nil {
		return rollback(err)
	}
	p, err := installedPlan(h, st)
	if err != nil {
		return rollback(err)
	}
	if err := proveExposureCertificate(ctx, p, 90*time.Second); err != nil {
		return rollback(err)
	}
	st.TLSReadiness = "verified"
	if err := saveState(h, st); err != nil {
		return rollback(err)
	}
	return journal.save(h, "complete")
}
