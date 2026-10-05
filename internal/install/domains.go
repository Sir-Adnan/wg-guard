package install

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/url"
	"path"
	"path/filepath"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/config"
	"github.com/Sir-Adnan/wg-guard/internal/domaintls"
	"github.com/Sir-Adnan/wg-guard/internal/layout"
)

// DomainOptions is application intent, not argv or arbitrary host path authority.
// Upload bytes are read from bounded private staging by the dedicated bridge.
type DomainOptions struct {
	Role             domaintls.Role
	Origin           string
	Method           domaintls.Method
	ExpectedRevision string
	Remove           bool
	CertPEM, KeyPEM  []byte
	Email            string
	Challenge        string         // http or cloudflare; credentials remain host-owned
	Roots            *x509.CertPool // isolated acceptance only; absent in CLI/web requests
	Stdout           io.Writer
	Progress         func(string)
}

type DomainInventory struct {
	Schema             int                 `json:"schema"`
	Revision           string              `json:"revision"`
	PanelOrigin        string              `json:"panel_origin"`
	SubscriptionOrigin string              `json:"subscription_origin"`
	Exposure           ExposureMode        `json:"exposure"`
	Available          bool                `json:"available"`
	Certificates       []DomainCertificate `json:"certificates"`
}

type DomainCertificate struct {
	Site  domaintls.Site            `json:"site"`
	Info  domaintls.CertificateInfo `json:"info"`
	State string                    `json:"state"`
}

func readDomainPolicy(h Host) (domaintls.Policy, error) {
	raw, err := readBoundedFile(h, layout.DomainPolicy, domaintls.MaxPolicyBytes)
	if err != nil {
		return domaintls.Policy{}, err
	}
	return domaintls.ReadPolicy(raw)
}

func legacyDomainPolicy(h Host, st *State, roots *x509.CertPool) (domaintls.Policy, error) {
	p, err := installedPlan(h, st)
	if err != nil {
		return domaintls.Policy{}, err
	}
	origin, err := domaintls.ParseOrigin(p.PublicURL())
	if err != nil {
		return domaintls.Policy{}, fmt.Errorf("domains: configure HTTPS panel access before managing independent domains")
	}
	method := domaintls.Manual
	if p.Certificate == CertificateBuiltin && p.TLSMode == config.TLSModeACME {
		method = domaintls.Builtin
	}
	if p.Exposure == ExposureNginx || p.Exposure == ExposureExternalProxy {
		method = domaintls.External
	}
	site := domaintls.Site{Role: domaintls.Panel, Origin: origin.URL, Method: method}
	if method == domaintls.Manual {
		certPEM, err := readBoundedFile(h, p.CertFile, domaintls.MaxMaterialBytes)
		if err != nil {
			return domaintls.Policy{}, err
		}
		defer clear(certPEM)
		keyPEM, err := readBoundedFile(h, p.KeyFile, domaintls.MaxMaterialBytes)
		if err != nil {
			return domaintls.Policy{}, err
		}
		defer clear(keyPEM)
		id, _, err := storeDomainPair(h, origin.Host, certPEM, keyPEM, roots)
		if err != nil {
			return domaintls.Policy{}, err
		}
		site.CertificateID = id
		if managedCertificateSource(p.Certificate) {
			site.Method = domaintls.Automatic
			site.LegacyLineage = st.Exposure.Lineage
			site.Challenge = "http"
			if p.Certificate == CertificateCloudflareDNS {
				site.Challenge = "cloudflare"
			}
		}
	}
	return domaintls.Policy{Schema: domaintls.Schema, Revision: "legacy", Sites: []domaintls.Site{site}}, nil
}

func DomainStatus(ctx context.Context, h Host) (DomainInventory, error) {
	st, err := LoadState(h)
	if err != nil || st == nil {
		return DomainInventory{}, fmt.Errorf("domains: installed state is unavailable")
	}
	p, err := readDomainPolicy(h)
	if errors.Is(err, fs.ErrNotExist) {
		plan, e := installedPlan(h, st)
		if e != nil {
			return DomainInventory{}, e
		}
		return DomainInventory{Schema: 1, Revision: "legacy", PanelOrigin: plan.PublicURL(), SubscriptionOrigin: plan.PublicURL(), Exposure: plan.Exposure, Available: st.Current != nil && st.Current.Contract.DomainProtocol == 1}, nil
	}
	if err != nil {
		return DomainInventory{}, err
	}
	i := DomainInventory{Schema: 1, Revision: p.Revision, PanelOrigin: p.PanelOrigin(), SubscriptionOrigin: p.SubscriptionOrigin(), Exposure: st.Exposure.Mode, Available: st.Current != nil && st.Current.Contract.DomainProtocol == 1}
	for _, site := range p.Sites {
		entry := DomainCertificate{Site: site, State: "active"}
		if site.Method == domaintls.External {
			entry.State = "external"
		}
		if site.Method == domaintls.Builtin {
			entry.State = "automatic"
		}
		if site.CertificateID != "" {
			certPath, _, _ := domaintls.PairFiles(layout.DomainPolicy, site.CertificateID)
			raw, e := readBoundedFile(h, certPath, domaintls.MaxMaterialBytes)
			if e != nil {
				entry.State = "unavailable"
			} else {
				info, e := inspectDomainLeaf(raw)
				if e != nil {
					entry.State = "unavailable"
				} else {
					entry.Info = info
					entry.Info.Automatic = site.Method == domaintls.Automatic
					if time.Until(info.NotAfter) < 30*24*time.Hour {
						entry.State = "expiring"
					}
				}
			}
		}
		i.Certificates = append(i.Certificates, entry)
	}
	return i, nil
}

func storeDomainPair(h Host, host string, certPEM, keyPEM []byte, roots *x509.CertPool) (string, domaintls.CertificateInfo, error) {
	_, info, err := domaintls.CheckPair(certPEM, keyPEM, []string{host}, time.Now(), roots)
	if err != nil {
		return "", info, err
	}
	id := transactionID()
	certFile, keyFile, err := domaintls.PairFiles(layout.DomainPolicy, id)
	if err != nil {
		return "", info, err
	}
	if err := h.MkdirAll(path.Join(layout.DomainDir, "certificates"), 0700); err != nil {
		return "", info, err
	}
	if err := h.MkdirAll(path.Dir(certFile), 0700); err != nil {
		return "", info, err
	}
	stored := false
	defer func() {
		if !stored {
			_ = h.RemoveAll(path.Dir(certFile))
		}
	}()
	if err := atomicWrite(h, keyFile, keyPEM, 0600); err != nil {
		return "", info, err
	}
	if err := atomicWrite(h, certFile, certPEM, 0644); err != nil {
		return "", info, err
	}
	stored = true
	return id, info, nil
}

// ConfigureDomain prepares/validates a candidate before stopping the existing
// listener. One lifecycle journal retains boot/Compose/policy recovery together.
func ConfigureDomain(ctx context.Context, h Host, o DomainOptions) (result DomainInventory, resultErr error) {
	if !h.IsRoot() {
		return result, terminalError("manage.root")
	}
	if !o.Role.Valid() || o.Remove && o.Role != domaintls.Subscription || !o.Remove && o.Method != domaintls.Automatic && o.Method != domaintls.Manual && o.Method != domaintls.External {
		return result, domaintls.ErrPolicy
	}
	unlock, err := h.LockLifecycle()
	if err != nil {
		return result, err
	}
	defer unlock()
	if err := noPending(h); err != nil {
		return result, err
	}
	st, err := LoadState(h)
	if err != nil || st == nil {
		return result, fmt.Errorf("domains: installed state is unavailable")
	}
	if st.Current == nil || st.Current.Contract.DomainProtocol != 1 {
		return result, fmt.Errorf("domains: installed runtime does not support the domain protocol")
	}
	current, err := installedPlan(h, st)
	if err != nil {
		return result, err
	}
	policy, err := readDomainPolicy(h)
	bootstrap := errors.Is(err, fs.ErrNotExist)
	if bootstrap {
		policy, err = domaintls.Policy{Revision: "legacy"}, nil
	}
	if err != nil {
		return result, err
	}
	if o.ExpectedRevision != policy.Revision {
		return result, fmt.Errorf("domains: active policy changed; review the current state before retrying")
	}
	operations := newOperationJournal(h)
	_ = operations.record(operationDomains, operationStarted, st.Mode)
	defer func() {
		outcome := operationSucceeded
		if resultErr != nil {
			outcome = operationFailed
		}
		_ = operations.record(operationDomains, outcome, st.Mode)
	}()
	if o.Progress != nil {
		o.Progress("validating")
	}
	before := cloneState(st)
	after := cloneState(st)
	j := &Journal{Schema: JournalSchema, ID: transactionID(), Operation: "domains", Before: &before, After: &after}
	if err := j.save(h, "prepared"); err != nil {
		return result, err
	}
	if err := createExposureBackup(h, j.ID); err != nil {
		_ = j.save(h, "aborted")
		return result, err
	}
	if err := j.save(h, "snapshot-ready"); err != nil {
		return result, err
	}
	committed := false
	activationStarted := false
	candidateLineage := ""
	defer func() {
		if resultErr != nil && !committed {
			if activationStarted {
				resultErr = errors.Join(resultErr, rollbackExposure(h, j))
			} else {
				resultErr = errors.Join(resultErr, j.save(h, "aborted"))
				_ = h.RemoveAll(exposureBackupDir(j.ID))
			}
		}
		if journal, e := LoadJournal(h); e == nil && (journal == nil || journal.terminal()) {
			_ = pruneDomainPairs(h)
			if resultErr != nil && !committed && candidateLineage != "" {
				cleanupCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
				defer cancel()
				resultErr = errors.Join(resultErr, removeUnreferencedDomainLineage(cleanupCtx, h, candidateLineage))
			}
		}
	}()
	if bootstrap {
		policy, err = legacyDomainPolicy(h, st, o.Roots)
		if err != nil {
			return result, err
		}
	}
	previousPolicy := policy
	var site domaintls.Site
	if !o.Remove {
		origin, e := domaintls.ParseOrigin(o.Origin)
		if e != nil {
			return result, e
		}
		site = domaintls.Site{Role: o.Role, Origin: origin.URL, Method: o.Method}
		if existing, ok := policy.Site(domaintls.Subscription); o.Role == domaintls.Panel && ok && origin.URL == existing.Origin {
			return result, fmt.Errorf("domains: target is the dedicated public origin; review/remove that role before using it for administration")
		}
		if current.Exposure == ExposureDirect && net.ParseIP(origin.Host) != nil && origin.URL != policy.PanelOrigin() {
			return result, fmt.Errorf("domains: independent direct HTTPS origins require DNS hostnames; retain existing IP access or use the explicit access setup flow")
		}
		if o.Role == domaintls.Subscription && origin.URL == policy.PanelOrigin() {
			return result, fmt.Errorf("domains: a separate subscription origin must differ from the panel; use remove to return to the shared origin")
		}
		if o.Method == domaintls.Automatic {
			site.Challenge = o.Challenge
		}
		if o.Method == domaintls.External {
			if current.Exposure != ExposureExternalProxy {
				return result, fmt.Errorf("domains: external TLS requires an operator-managed reverse proxy; owned single-domain Nginx must be reconfigured explicitly first")
			}
		} else {
			if current.Exposure != ExposureDirect {
				return result, fmt.Errorf("domains: managed certificates require direct HTTPS; use external TLS for a proxy-owned listener")
			}
			if origin.Port != current.PanelPort {
				return result, fmt.Errorf("domains: direct hostnames must use the existing HTTPS listener port")
			}
			certPEM, keyPEM := o.CertPEM, o.KeyPEM
			if o.Method == domaintls.Automatic && (len(certPEM) == 0 || len(keyPEM) == 0) {
				candidateLineage = DomainLineage(origin.Host)
				if o.Progress != nil {
					o.Progress("issuing")
				}
				certPEM, keyPEM, e = issueDomainCertificate(ctx, h, current, origin.Host, o)
				if e != nil {
					return result, e
				}
				defer clear(certPEM)
				defer clear(keyPEM)
			}
			if o.Progress != nil {
				o.Progress("issued")
			}
			id, _, e := storeDomainPair(h, origin.Host, certPEM, keyPEM, o.Roots)
			if e != nil {
				return result, e
			}
			site.CertificateID = id
			if old, ok := policy.Site(o.Role); ok && old.Origin == site.Origin && old.Method == domaintls.Automatic && o.Method == domaintls.Automatic && len(o.CertPEM) > 0 {
				site.LegacyLineage = old.LegacyLineage
				site.Challenge = old.Challenge
			}
		}
	}
	// Pin the old shared customer origin when the panel name changes. A role
	// change reuses approved material; it never rotates tokens or device keys.
	if !o.Remove && o.Role == domaintls.Panel && site.Origin != policy.PanelOrigin() {
		if _, exists := policy.Site(domaintls.Subscription); !exists {
			previousPanel, _ := policy.Site(domaintls.Panel)
			previousPanel.Role = domaintls.Subscription
			policy.Sites = append(policy.Sites, previousPanel)
		}
	}
	sites := make([]domaintls.Site, 0, 2)
	for _, existing := range policy.Sites {
		if existing.Role != o.Role {
			sites = append(sites, existing)
		}
	}
	if !o.Remove {
		sites = append(sites, site)
	}
	policy = domaintls.Policy{Schema: domaintls.Schema, Revision: transactionID(), Sites: sites}
	if err := policy.Validate(); err != nil {
		return result, err
	}
	panel, _ := policy.Site(domaintls.Panel)
	panelOrigin, _ := domaintls.ParseOrigin(panel.Origin)
	if current.Exposure == ExposureDirect {
		builtin := false
		for _, active := range policy.Sites {
			builtin = builtin || active.Method == domaintls.Builtin
		}
		if builtin {
			current.TLSMode = config.TLSModeACME
		} else {
			current.TLSMode = config.TLSModeManual
		}
		current.Domain = panelOrigin.Host
		current.DomainPolicyFile = layout.DomainPolicy
		current.DomainChallengeDir = layout.DomainChallenges
		current.CertFile, current.KeyFile = "", ""
		after.Exposure.PublicURL = panel.Origin
		// Domain policy owns certificate state; the legacy exposure is kept only
		// for access topology. No independent lineage/key-path inference is made.
		after.Exposure.Certificate = CertificateDomains
		after.Exposure.CertFile, after.Exposure.KeyFile, after.Exposure.Lineage, after.Exposure.DeployHook = "", "", "", ""
		for _, active := range policy.Sites {
			if active.Method == domaintls.Automatic && active.Challenge == "cloudflare" {
				after.Exposure.CredentialsFile = CloudflareTokenPath
			}
		}
	} else {
		current.Domain = panelOrigin.Host
		current.PublicPort = panelOrigin.Port
		current.DomainPolicyFile = layout.DomainPolicy
		after.Exposure.PublicURL = panel.Origin
		after.Exposure.PublicPort = panelOrigin.Port
	}
	if err := j.save(h, "swap-pending"); err != nil {
		return result, err
	}
	if o.Progress != nil {
		o.Progress("activating")
	}
	activationStarted = true
	if err := stopService(ctx, h, &before); err != nil {
		return result, err
	}
	raw, _ := json.Marshal(policy)
	if err := atomicWrite(h, layout.DomainPolicy, raw, 0600); err != nil {
		return result, err
	}
	if err := writeAccessRuntime(ctx, h, current, &after); err != nil {
		return result, err
	}
	if err := j.save(h, "started"); err != nil {
		return result, err
	}
	if err := startService(ctx, h, &after); err != nil {
		return result, err
	}
	if err := waitHealthyRecorded(ctx, h, &after, updateHealthWindow, io.Discard); err != nil {
		return result, err
	}
	for _, active := range policy.Sites {
		if active.Method == domaintls.External {
			continue
		}
		activeOrigin, _ := domaintls.ParseOrigin(active.Origin)
		probe := current
		probe.Domain = activeOrigin.Host
		if err := proveDomainCertificate(ctx, h, probe, o.Roots); err != nil {
			return result, err
		}
	}
	after.TLSReadiness = "verified"
	if current.Exposure == ExposureExternalProxy {
		after.TLSReadiness = "external-unverified"
	}
	if err := saveState(h, &after); err != nil {
		return result, err
	}
	j.After = &after
	if err := j.save(h, "complete"); err != nil {
		return result, err
	}
	committed = true
	if o.Progress != nil {
		o.Progress("verified")
	}
	_ = h.RemoveAll(exposureBackupDir(j.ID))
	if err := retireDomainLineages(ctx, h, previousPolicy, policy); err != nil {
		return result, err
	}
	return DomainStatus(ctx, h)
}

var proveDomainCertificate = func(ctx context.Context, h Host, p Plan, roots *x509.CertPool) error {
	return waitCertificate(ctx, p, 30*time.Second, func(ctx context.Context) error { return probeCertificate(ctx, p, roots) })
}

func DomainLineage(host string) string { return domaintls.DomainLineage(host) }

func issueDomainCertificate(ctx context.Context, h Host, current Plan, host string, o DomainOptions) ([]byte, []byte, error) {
	if net.ParseIP(host) != nil || o.Email != "" && !validACMEEmail(o.Email) || o.Challenge != "http" && o.Challenge != "cloudflare" {
		return nil, nil, domaintls.ErrPolicy
	}
	if err := ensureCertbot(ctx, h, o.Challenge == "cloudflare"); err != nil {
		return nil, nil, err
	}
	lineage := DomainLineage(host)
	args := []string{CertbotPath, "certonly", "--non-interactive", "--agree-tos", "--keep-until-expiring", "--no-directory-hooks", "--cert-name", lineage, "-d", host}
	if o.Email != "" {
		args = append(args, "--email", o.Email)
	} else {
		args = append(args, "--register-unsafely-without-email")
	}
	if o.Challenge == "cloudflare" {
		info, err := h.Stat(CloudflareTokenPath)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return nil, nil, fmt.Errorf("domains: managed DNS credentials are unavailable")
		}
		args = append(args, "--dns-cloudflare", "--dns-cloudflare-credentials", CloudflareTokenPath, "--dns-cloudflare-propagation-seconds", "30")
	} else if domainChallengeListenerActive(h, current) {
		enrollment := domaintls.Enrollment{Schema: domaintls.Schema, Hosts: []string{host}, ExpiresAt: time.Now().Add(15 * time.Minute)}
		if err := writeJSON(h, filepath.Join(layout.DomainChallenges, "enrollment.json"), enrollment); err != nil {
			return nil, nil, err
		}
		defer h.Remove(filepath.Join(layout.DomainChallenges, "enrollment.json"))
		args = append(args, "--webroot", "--webroot-path", layout.DomainChallenges)
	} else if h.PortFree(":80") {
		args = append(args, "--standalone", "--http-01-port", "80")
	} else {
		return nil, nil, fmt.Errorf("domains: HTTP-01 port is not owned/available; use managed DNS credentials or external TLS")
	}
	if err := runQuiet(ctx, h, args, longTimeout); err != nil {
		return nil, nil, fmt.Errorf("domains: automatic issuance failed")
	}
	if err := ensureDomainRenewalHooks(h); err != nil {
		return nil, nil, err
	}
	return readDomainPairFiles(h, CertbotLivePath(lineage)+"/fullchain.pem", CertbotLivePath(lineage)+"/privkey.pem", lineage)
}

func domainChallengeListenerActive(h Host, p Plan) bool {
	if p.DomainChallengeDir == "" {
		return false
	}
	if p.TLSMode == config.TLSModeACME {
		return true
	}
	policy, err := readDomainPolicy(h)
	if err != nil {
		return false
	}
	for _, site := range policy.Sites {
		if site.Method == domaintls.Automatic {
			return true
		}
	}
	return false
}

func RecoverDomains(ctx context.Context, h Host) error {
	if !h.IsRoot() {
		return terminalError("manage.root")
	}
	unlock, err := h.LockLifecycle()
	if err != nil {
		return err
	}
	defer unlock()
	j, err := LoadJournal(h)
	if err != nil {
		return err
	}
	if j == nil || j.terminal() {
		return pruneDomainPairs(h)
	}
	if j.Operation != "domains" {
		return pendingOperationError(j)
	}
	if err := rollbackExposure(h, j); err != nil {
		return err
	}
	return pruneDomainPairs(h)
}

// ControlledDomainSource admits only the fixed import directory or a recorded
// legacy certificate lineage. A web request cannot browse arbitrary root files.
func ControlledDomainSource(st *State, role domaintls.Role, cert, key string) bool {
	if !role.Valid() {
		return false
	}
	base := filepath.Join(layout.ConfigDir, "certificate-import", string(role))
	if cert == filepath.Join(base, "fullchain.pem") && key == filepath.Join(base, "privkey.pem") {
		return true
	}
	return st != nil && st.Exposure.Lineage != "" && cert == CertbotLivePath(st.Exposure.Lineage)+"/fullchain.pem" && key == CertbotLivePath(st.Exposure.Lineage)+"/privkey.pem"
}

func domainOriginHost(value string) string {
	u, _ := url.Parse(value)
	if u == nil {
		return ""
	}
	return u.Hostname()
}

func inspectDomainLeaf(raw []byte) (domaintls.CertificateInfo, error) {
	block, _ := pem.Decode(raw)
	if block == nil || block.Type != "CERTIFICATE" {
		return domaintls.CertificateInfo{}, domaintls.ErrMaterial
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return domaintls.CertificateInfo{}, domaintls.ErrMaterial
	}
	hash := sha256.Sum256(leaf.Raw)
	return domaintls.CertificateInfo{Fingerprint: hex.EncodeToString(hash[:]), NotBefore: leaf.NotBefore, NotAfter: leaf.NotAfter, Issuer: leaf.Issuer.CommonName}, nil
}
