package install

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"time"
)

type ReconfigureOptions struct {
	Plan   Plan
	Stdout io.Writer
}

type exposureBackupEntry struct {
	Path   string `json:"path"`
	Name   string `json:"name"`
	Mode   uint32 `json:"mode,omitempty"`
	Exists bool   `json:"exists"`
}

type exposureBackupManifest struct {
	Schema        int                   `json:"schema"`
	Files         []exposureBackupEntry `json:"files"`
	WebrootExists bool                  `json:"webroot_exists"`
}

var exposureBackupFiles = []struct{ path, name string }{
	{ConfigPath, "boot-config"},
	{ComposePth, "compose"},
	{UnitPath, "systemd-unit"},
	{NginxConfigPath, "nginx"},
	{ManagedCertPath, "certificate"},
	{ManagedKeyPath, "private-key"},
	{CloudflareTokenPath, "cloudflare-credentials"},
	{CertbotDeployHookPath, "deploy-hook"},
}

func exposureBackupDir(id string) string { return ArtifactDir + "/" + id + "/exposure" }

// InstalledPlan returns the non-secret installed access plan.
func InstalledPlan(h Host, st *State) (Plan, error) { return installedPlan(h, st) }

func resolveReconfigurePlan(ctx context.Context, h Host, current, requested Plan) (Plan, error) {
	requested.Mode = current.Mode
	requested.Image = current.Image
	requested.EtcDir = EtcDir
	requested.DataDir = DataDir
	if requested.PublicIP == "" {
		requested.PublicIP = current.PublicIP
	}
	if requested.PanelPort == 0 {
		requested.PanelPort = current.PanelPort
	}
	if requested.PublicPort == 0 {
		requested.PublicPort = 443
	}
	if requested.ACMEHTTPPort == 0 {
		requested.ACMEHTTPPort = 80
	}
	facts, err := InspectExposure(ctx, h, requested.Domain)
	if err != nil {
		return Plan{}, err
	}
	if current.Exposure == ExposureNginx && requested.Domain == current.Domain {
		facts.NginxDomainConflict = false
	}
	if current.Exposure == ExposureDirect {
		if current.PanelPort == 443 {
			facts.HTTPSPortFree = true
		}
		if current.Certificate == CertificateBuiltin && current.ACMEHTTPPort == 80 {
			facts.HTTPPortFree = true
		}
	}
	if current.Exposure == ExposurePrivate || current.Exposure == ExposureNginx || current.Exposure == ExposureExternalProxy {
		facts.BackendPort = current.PanelPort
	}
	resolved, err := ResolveExposure(requested, facts)
	if err != nil {
		return Plan{}, err
	}
	resolved, err = resolved.Resolve()
	if err != nil {
		return Plan{}, err
	}
	currentOwnsPort := current.PanelPort == resolved.PanelPort
	listen := "127.0.0.1:" + fmt.Sprint(resolved.PanelPort)
	if resolved.Exposure == ExposureDirect {
		listen = ":" + fmt.Sprint(resolved.PanelPort)
	}
	if !currentOwnsPort && !h.PortFree(listen) {
		return Plan{}, fmt.Errorf("installer: requested panel port %d is already in use", resolved.PanelPort)
	}
	return resolved, nil
}

// ResolveReconfigurePlan derives and validates a proposed access plan without
// mutating the host. The lifecycle service repeats this check before commit.
func ResolveReconfigurePlan(ctx context.Context, h Host, current, requested Plan) (Plan, error) {
	return resolveReconfigurePlan(ctx, h, current, requested)
}

func equivalentAccess(a, b Plan) bool {
	return a.Exposure == b.Exposure && a.Certificate == b.Certificate && a.Domain == b.Domain &&
		a.PublicIP == b.PublicIP && a.PanelPort == b.PanelPort && a.PublicPort == b.PublicPort &&
		a.ACMEHTTPPort == b.ACMEHTTPPort && a.TLSMode == b.TLSMode &&
		a.CertFile == b.CertFile && a.KeyFile == b.KeyFile && b.CloudflareToken == "" && b.ACMEEmail == ""
}

func cloneState(st *State) State {
	copy := *st
	copy.RepositoryChanges = append([]string(nil), st.RepositoryChanges...)
	copy.ExtraFiles = append([]string(nil), st.ExtraFiles...)
	copy.PackagesInstalled = append([]string(nil), st.PackagesInstalled...)
	return copy
}

func createExposureBackup(h Host, id string) (resultErr error) {
	dir := exposureBackupDir(id)
	if err := h.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	defer func() {
		if resultErr != nil {
			_ = h.RemoveAll(dir)
		}
	}()
	manifest := exposureBackupManifest{Schema: 1}
	for _, item := range exposureBackupFiles {
		snapshot, err := captureFile(h, item.path, 1<<20)
		if err != nil {
			return err
		}
		entry := exposureBackupEntry{Path: item.path, Name: item.name, Mode: uint32(snapshot.mode.Perm()), Exists: snapshot.exists}
		manifest.Files = append(manifest.Files, entry)
		if snapshot.exists {
			if err := atomicWrite(h, dir+"/"+item.name, snapshot.data, 0o600); err != nil {
				return err
			}
		}
	}
	if info, err := h.Stat(ACMEWebrootPath); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("installer: managed ACME webroot is not a directory")
		}
		manifest.WebrootExists = true
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return writeJSON(h, dir+"/manifest.json", manifest)
}

func loadExposureBackup(h Host, id string) (exposureBackupManifest, error) {
	dir := exposureBackupDir(id)
	raw, err := readRecord(h, dir+"/manifest.json")
	if err != nil {
		return exposureBackupManifest{}, err
	}
	var manifest exposureBackupManifest
	if json.Unmarshal(raw, &manifest) != nil || manifest.Schema != 1 || len(manifest.Files) != len(exposureBackupFiles) {
		return manifest, fmt.Errorf("installer: invalid access rollback manifest")
	}
	for i, expected := range exposureBackupFiles {
		entry := manifest.Files[i]
		if entry.Path != expected.path || entry.Name != expected.name || path.Base(entry.Name) != entry.Name || entry.Mode > 0o777 {
			return manifest, fmt.Errorf("installer: unsafe access rollback manifest")
		}
	}
	return manifest, nil
}

func restoreExposureBackup(h Host, id string) error {
	manifest, err := loadExposureBackup(h, id)
	if err != nil {
		return err
	}
	dir := exposureBackupDir(id)
	for _, entry := range manifest.Files {
		if !entry.Exists {
			if err := h.Remove(entry.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			continue
		}
		data, err := readBoundedFile(h, dir+"/"+entry.Name, 1<<20)
		if err != nil {
			return err
		}
		if err := atomicWrite(h, entry.Path, data, fs.FileMode(entry.Mode)); err != nil {
			clear(data)
			return err
		}
		clear(data)
	}
	if manifest.WebrootExists {
		if err := h.MkdirAll(ACMEWebrootPath, 0o755); err != nil {
			return err
		}
	} else if err := h.RemoveAll(ACMEWebrootPath); err != nil {
		return err
	}
	return nil
}

func writeAccessRuntime(ctx context.Context, h Host, p Plan, st *State) error {
	boot, err := renderBootConfig(p)
	if err != nil {
		return err
	}
	if err := atomicWrite(h, ConfigPath, boot, 0o600); err != nil {
		return err
	}
	if st.Mode == ModeDocker {
		if p.Image == "" {
			current, readErr := h.ReadFile(ComposePth)
			if readErr != nil {
				return readErr
			}
			p.Image = imageFromCompose(string(current))
		}
		if p.Image == "" {
			return fmt.Errorf("installer: installed Docker image is not recorded")
		}
		return atomicWrite(h, ComposePth, []byte(RenderCompose(p)), 0o644)
	}
	if err := atomicWrite(h, UnitPath, []byte(RenderUnit(p)), 0o644); err != nil {
		return err
	}
	return h.Run(ctx, []string{"systemctl", "daemon-reload"}, 30*time.Second)
}

func exposureTLSReadiness(p Plan) string {
	switch {
	case p.Exposure == ExposurePrivate:
		return "not-applicable"
	case p.Certificate == CertificateExternal:
		return "external-unverified"
	case p.Certificate == CertificateCloudflareOrigin:
		return "origin-proxy-unverified"
	default:
		return "pending"
	}
}

func retireExposureArtifacts(h Host, before, after ExposureState) error {
	keep := map[string]bool{}
	for _, p := range managedExposureArtifacts(after) {
		keep[p] = true
	}
	for _, p := range managedExposureArtifacts(before) {
		if keep[p] || p == NginxConfigPath || p == ACMEWebrootPath {
			continue
		}
		if err := h.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	if before.ACMEWebroot != "" && after.ACMEWebroot == "" {
		return h.RemoveAll(ACMEWebrootPath)
	}
	return nil
}

func reactivateNginxForExposure(ctx context.Context, h Host, before, after *State) error {
	if (before == nil || before.Exposure.Mode != ExposureNginx) && (after == nil || after.Exposure.Mode != ExposureNginx) {
		return nil
	}
	status, err := h.Output(ctx, []string{"systemctl", "is-active", "nginx.service"}, 10*time.Second)
	if err != nil || strings.TrimSpace(status) != "active" {
		return fmt.Errorf("installer: managed Nginx is not active during access recovery")
	}
	return validateReloadNginx(ctx, h, true)
}

func rollbackExposure(h Host, journal *Journal) error {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	if journal.Before == nil {
		return errors.Join(fmt.Errorf("installer: access recovery has no prior state"), journal.save(h, "recovery-required"))
	}
	stopErr := stopService(ctx, h, journal.Before)
	restoreErr := restoreExposureBackup(h, journal.ID)
	if restoreErr == nil && journal.Before.Mode == ModeNative {
		restoreErr = h.Run(ctx, []string{"systemctl", "daemon-reload"}, 30*time.Second)
	}
	if restoreErr == nil {
		restoreErr = reactivateNginxForExposure(ctx, h, journal.Before, journal.After)
	}
	startErr := error(nil)
	if restoreErr == nil {
		startErr = startService(ctx, h, journal.Before)
		if startErr == nil {
			startErr = waitHealthyRecorded(ctx, h, journal.Before, updateHealthWindow, io.Discard)
		}
	}
	if restoreErr != nil || startErr != nil {
		journal.Before.Recovery = "exposure-recovery-required"
		_ = saveState(h, journal.Before)
		return errors.Join(stopErr, restoreErr, startErr, journal.save(h, "recovery-required"))
	}
	journal.Before.Recovery = ""
	if err := saveState(h, journal.Before); err != nil {
		return errors.Join(stopErr, err, journal.save(h, "recovery-required"))
	}
	if err := journal.save(h, "rolled-back"); err != nil {
		return errors.Join(stopErr, err)
	}
	if err := h.RemoveAll(exposureBackupDir(journal.ID)); err != nil {
		return errors.Join(stopErr, err)
	}
	return stopErr
}

// ReconfigureExposure changes only panel access/TLS while retaining the
// installed build, deployment mode, data and AmneziaWG state.
func ReconfigureExposure(ctx context.Context, h Host, o ReconfigureOptions) (result *State, resultErr error) {
	if !h.IsRoot() {
		return nil, terminalError("manage.root")
	}
	unlock, err := h.LockLifecycle()
	if err != nil {
		return nil, err
	}
	defer unlock()
	st, err := LoadState(h)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, terminalError("install.error.health.3")
	}
	if err := noPending(h); err != nil {
		return nil, err
	}
	current, err := installedPlan(h, st)
	if err != nil {
		return nil, err
	}
	if err := waitHealthyRecorded(ctx, h, st, 15*time.Second, io.Discard); err != nil {
		return nil, fmt.Errorf("installer: current node must be healthy before changing panel access: %w", err)
	}
	candidate, err := resolveReconfigurePlan(ctx, h, current, o.Plan)
	if err != nil {
		return nil, err
	}
	if equivalentAccess(current, candidate) {
		return st, nil
	}

	before := cloneState(st)
	next := cloneState(st)
	next.Schema = StateSchema
	next.PublicIP = candidate.PublicIP
	next.Exposure = candidate.ExposureRecord()
	next.TLSReadiness = exposureTLSReadiness(candidate)
	next.Recovery = ""
	journal := &Journal{Schema: 1, ID: transactionID(), Operation: "exposure", Before: &before, After: &next}
	if err := journal.save(h, "prepared"); err != nil {
		return nil, err
	}
	if err := createExposureBackup(h, journal.ID); err != nil {
		_ = journal.save(h, "rolled-back")
		return nil, err
	}
	if err := journal.save(h, "snapshot-ready"); err != nil {
		_ = h.RemoveAll(exposureBackupDir(journal.ID))
		return nil, err
	}
	committed := false
	defer func() {
		if resultErr != nil && !committed {
			resultErr = errors.Join(resultErr, rollbackExposure(h, journal))
			result = &before
		}
	}()

	if candidate.Exposure == ExposureNginx {
		if current.Exposure == ExposureNginx {
			if _, err := PrepareNginxReplacement(ctx, h, candidate, current, &next, o.Stdout); err != nil {
				return nil, err
			}
		} else if _, err := PrepareNginx(ctx, h, candidate, &next, o.Stdout); err != nil {
			return nil, err
		}
	}
	certificate, _, err := PrepareCertificate(ctx, h, candidate, &next, o.Stdout)
	if err != nil {
		return nil, err
	}
	if certificate.Managed {
		candidate.CertFile, candidate.KeyFile = certificate.CertFile, certificate.KeyFile
		next.Exposure = candidate.ExposureRecord()
		next.Exposure.Lineage = certificate.Lineage
	}
	if _, err := PrepareCertificateHook(h, candidate); err != nil {
		return nil, err
	}
	candidate.CloudflareToken = ""
	if candidate.Exposure == ExposureNginx {
		if current.Exposure == ExposureNginx {
			if err := FinalizeNginxReplacement(ctx, h, candidate, current, o.Stdout); err != nil {
				return nil, err
			}
		} else if err := FinalizeNginx(ctx, h, candidate, o.Stdout); err != nil {
			return nil, err
		}
	}
	if err := journal.save(h, "swap-pending"); err != nil {
		return nil, err
	}
	if err := stopService(ctx, h, &before); err != nil {
		return nil, err
	}
	if current.Exposure == ExposureNginx && candidate.Exposure != ExposureNginx {
		if _, err := DetachManagedNginx(ctx, h, before.Exposure); err != nil {
			return nil, err
		}
	}
	if err := writeAccessRuntime(ctx, h, candidate, &next); err != nil {
		return nil, err
	}
	if err := journal.save(h, "started"); err != nil {
		return nil, err
	}
	if err := startService(ctx, h, &next); err != nil {
		return nil, err
	}
	if err := waitHealthyRecorded(ctx, h, &next, updateHealthWindow, io.Discard); err != nil {
		return nil, err
	}
	if next.TLSReadiness == "pending" {
		if err := proveExposureCertificate(ctx, candidate, 90*time.Second); err != nil {
			return nil, err
		}
		next.TLSReadiness = "verified"
	}
	if err := retireExposureArtifacts(h, before.Exposure, next.Exposure); err != nil {
		return nil, err
	}
	if err := saveState(h, &next); err != nil {
		return nil, err
	}
	journal.After = &next
	if err := journal.save(h, "complete"); err != nil {
		return nil, err
	}
	committed = true
	if err := h.RemoveAll(exposureBackupDir(journal.ID)); err != nil {
		return &next, fmt.Errorf("installer: access changed, but private rollback cleanup failed: %w", err)
	}
	return &next, nil
}

// RecoverExposure restores the durable pre-change snapshot for an interrupted
// access transaction. Prepared-only transactions made no runtime mutation.
func RecoverExposure(_ context.Context, h Host) error {
	if !h.IsRoot() {
		return terminalError("manage.root")
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
	if journal == nil || journal.terminal() || journal.Operation != "exposure" {
		return fmt.Errorf("installer: no interrupted panel-access change is recorded")
	}
	if journal.Stage == "prepared" {
		_ = h.RemoveAll(exposureBackupDir(journal.ID))
		return journal.save(h, "rolled-back")
	}
	return rollbackExposure(h, journal)
}
