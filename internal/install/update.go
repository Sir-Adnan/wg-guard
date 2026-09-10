package install

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"time"
)

// Update verifies before changing the active deployment. Every step after
// swap-pending is recoverable, including state persistence and cancellation.
func Update(ctx context.Context, h Host, o UpdateOptions) (resultErr error) {
	out := o.Stdout
	if out == nil {
		out = io.Discard
	}
	o.Stdout = out
	if !h.IsRoot() {
		return terminalError("install.error.root")
	}
	unlock, err := h.LockLifecycle()
	if err != nil {
		return err
	}
	defer unlock()
	st, err := LoadState(h)
	if err != nil {
		return err
	}
	j, err := LoadJournal(h)
	if err != nil {
		return err
	}
	if j != nil && !j.terminal() {
		if !o.Rollback && !o.Recover {
			return pendingOperationError(j)
		}
		return recoverTransaction(h, j, o.Stdout)
	}
	if o.Recover {
		return nil
	}
	if st == nil {
		return terminalError("install.error.no_state")
	}
	if err := migrateLegacyExposure(h, st); err != nil {
		return err
	}
	if err := ensureOperationRetention(ctx, h, st, true); err != nil {
		return err
	}
	if st.Mode == ModeNative {
		if err := requireOwnedOrAbsent(h, st, JournalRetentionPath); err != nil {
			return err
		}
	}
	action := operationUpdate
	if o.Rollback {
		action = operationRollback
	}
	operations := newOperationJournal(h)
	_ = operations.record(action, operationStarted, st.Mode)
	defer func() {
		outcome := operationSucceeded
		if resultErr != nil {
			outcome = operationFailed
		}
		_ = operations.record(action, outcome, st.Mode)
	}()
	step(out, "Update")
	progress(out, "update_prepare")
	j = &Journal{Schema: 1, ID: transactionID(), Operation: "update", Before: st}
	previous, err := retainCurrent(ctx, h, st)
	if err != nil {
		return err
	}
	j.Previous = previous
	recorded := false
	defer func() {
		if !recorded {
			removeArtifact(h, previous)
			if !o.Rollback {
				removeArtifact(h, j.Candidate)
			}
		}
	}()
	if o.Rollback {
		j.Operation = "rollback"
		if st.Previous == nil {
			return terminalError("install.error.no_previous")
		}
		j.Candidate = st.Previous
	} else {
		j.Candidate, err = stageCandidate(ctx, h, st, o)
		if err != nil {
			return err
		}
	}
	if o.Rollback && !dataCompatible(previous, j.Candidate) {
		return terminalError("install.error.rollback_restore")
	}
	if o.SkipBackup && !dataCompatible(previous, j.Candidate) {
		return terminalError("install.error.backup_required")
	}
	if !o.SkipBackup && !o.Rollback {
		previous.Backup, err = createBackup(ctx, h, st, j.ID)
		if err != nil {
			return err
		}
	}
	next := *st
	next.Schema = StateSchema
	next.Current = j.Candidate
	next.Previous = previous
	next.Image = j.Candidate.Image
	next.Version = j.Candidate.Build.Version
	next.Recovery = ""
	next.ExtraFiles = append([]string(nil), st.ExtraFiles...)
	if st.Mode == ModeNative && j.Candidate.Unit != "" {
		unit, readErr := h.ReadFile(j.Candidate.Unit)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(unit), "LogNamespace=wg-guard") {
			next.ExtraFiles = addUnique(next.ExtraFiles, JournalRetentionPath)
		} else {
			filtered := next.ExtraFiles[:0]
			for _, file := range next.ExtraFiles {
				if file != JournalRetentionPath {
					filtered = append(filtered, file)
				}
			}
			next.ExtraFiles = filtered
		}
	}
	j.After = &next
	if err = j.save(h, "prepared"); err != nil {
		return err
	}
	recorded = true
	if err = ctx.Err(); err != nil {
		_ = j.save(h, "aborted")
		return err
	}
	// systemd may restart independently immediately after a binary rename.
	j.DataMayHaveChanged = true
	if err = j.save(h, "swap-pending"); err != nil {
		return err
	}
	step(out, "Applying update")
	fail := func(cause error) error {
		return errors.Join(cause, recoverTransaction(h, j, o.Stdout), terminalError("install.error.update_failed"))
	}
	if err = deployArtifact(h, st, j.Candidate); err != nil {
		return fail(err)
	}
	// A failed start command may already have run migrations: journal first.
	if err = j.save(h, "started"); err != nil {
		return fail(err)
	}
	if err = startService(ctx, h, st); err != nil {
		return fail(err)
	}
	if err = waitHealthyRecorded(ctx, h, st, updateHealthWindow, o.Stdout); err != nil {
		return fail(err)
	}
	if err = saveState(h, &next); err != nil {
		return fail(err)
	}
	if err = j.save(h, "complete"); err != nil {
		return fail(err)
	}
	for _, old := range []*Artifact{st.Current, st.Previous} {
		if old != nil && old.Binary != previous.Binary && old.Binary != j.Candidate.Binary {
			removeArtifact(h, old)
		}
	}
	progress(out, "update_complete")
	return nil
}

func requireOwnedOrAbsent(h Host, state *State, target string) error {
	if _, ok := h.(realHost); ok {
		if err := safeHostPath(target); err != nil {
			return err
		}
	}
	info, err := h.Stat(target)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	owned := false
	for _, file := range state.ExtraFiles {
		owned = owned || file == target
	}
	if !owned || !info.Mode().IsRegular() {
		return fmt.Errorf("update: refusing unowned policy %s", target)
	}
	return nil
}

// migrateLegacyExposure upgrades only topology that can be reconstructed
// exactly from the authoritative boot configuration. It intentionally does
// not claim ownership of arbitrary legacy manual-certificate paths.
func migrateLegacyExposure(h Host, st *State) error {
	if st.Exposure != (ExposureState{}) {
		return nil
	}
	p, err := installedPlan(h, st)
	if err != nil {
		return err
	}
	if p.Certificate == CertificateManual || p.Certificate == CertificateCloudflareOrigin {
		if p.CertFile != ManagedCertPath || p.KeyFile != ManagedKeyPath {
			return nil
		}
	}
	record := p.ExposureRecord()
	if err := validateExposureState(record); err != nil {
		return err
	}
	st.Exposure = record
	if st.TLSReadiness == "" {
		st.TLSReadiness = exposureTLSReadiness(p)
	}
	return nil
}

// Only exact owned files and their empty private directory may be pruned.
// Docker images and backups may be shared/recovery resources and are retained.
func removeArtifact(h Host, a *Artifact) {
	if a == nil || validateArtifact(a) != nil {
		return
	}
	if _, ok := h.(realHost); ok {
		if safeHostPath(a.Binary) != nil || a.Compose != "" && safeHostPath(a.Compose) != nil || a.Unit != "" && safeHostPath(a.Unit) != nil {
			return
		}
	}
	_ = h.Remove(a.Binary)
	if a.Compose != "" {
		_ = h.Remove(a.Compose)
	}
	if a.Unit != "" {
		_ = h.Remove(a.Unit)
	}
	_ = h.Remove(path.Dir(a.Binary))
}
func dataCompatible(a, b *Artifact) bool {
	return a != nil && b != nil && knownDataContract(a.Contract) && knownDataContract(b.Contract) && a.Contract.DataContract == b.Contract.DataContract
}

func retainCurrent(ctx context.Context, h Host, st *State) (result *Artifact, resultErr error) {
	dir := ArtifactDir + "/" + transactionID()
	if err := h.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	a := &Artifact{Binary: dir + "/binary"}
	defer func() {
		if resultErr != nil {
			_ = h.Remove(dir + "/binary")
			_ = h.Remove(dir + "/compose.yaml")
			_ = h.Remove(dir + "/wg-guard.service")
			_ = h.Remove(dir)
		}
	}()
	if st.Current != nil {
		a.Build = st.Current.Build
	}
	if err := h.CopyFile(BinPath, a.Binary, 0755); err != nil {
		return nil, err
	}
	digest, _, err := fileDigest(ctx, h, a.Binary, 256<<20)
	if err != nil {
		return nil, err
	}
	a.BinarySHA256 = digest
	args := []string{BinPath}
	if st.Mode == ModeDocker {
		raw, err := h.Output(ctx, []string{"docker", "inspect", "--format", "{{.Image}}", Container}, 30*time.Second)
		if err != nil {
			return nil, err
		}
		a.Image = strings.TrimSpace(raw)
		if !imageID(a.Image) {
			return nil, terminalError("install.error.image_identity")
		}
		a.Compose = dir + "/compose.yaml"
		b, err := h.ReadFile(ComposePth)
		if err != nil {
			return nil, err
		}
		b, err = composeImage(b, a.Image)
		if err != nil {
			return nil, err
		}
		if err = atomicWrite(h, a.Compose, b, 0600); err != nil {
			return nil, err
		}
		args = []string{"docker", "exec", Container, BinPath}
	} else {
		a.Unit = dir + "/wg-guard.service"
		if err := h.CopyFile(UnitPath, a.Unit, 0o600); err != nil {
			return nil, err
		}
	}
	// Absent legacy contract never proves interpretation compatibility.
	a.Contract, _ = readContract(ctx, h, args)
	return a, nil
}

func stageCandidate(ctx context.Context, h Host, st *State, o UpdateOptions) (result *Artifact, resultErr error) {
	binary := o.BinaryPath
	if o.Build.BinaryPath != "" {
		binary = o.Build.BinaryPath
	}
	if binary == "" {
		return nil, terminalError("install.error.binary")
	}
	dir := ArtifactDir + "/" + transactionID()
	if err := h.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	a := &Artifact{Build: o.Build, Binary: dir + "/binary"}
	defer func() {
		if resultErr != nil {
			_ = h.Remove(dir + "/binary")
			_ = h.Remove(dir + "/compose.yaml")
			_ = h.Remove(dir + "/wg-guard.service")
			_ = h.Remove(dir)
		}
	}()
	if err := h.CopyFile(binary, a.Binary, 0755); err != nil {
		return nil, err
	}
	digest, _, err := fileDigest(ctx, h, a.Binary, 256<<20)
	if err != nil {
		return nil, err
	}
	a.BinarySHA256 = digest
	if o.Build.SHA256 != "" && digest != o.Build.SHA256 {
		return nil, terminalError("install.error.image.5")
	}
	a.Build.BinaryPath = ""
	a.Build.SHA256 = digest
	a.Contract, err = inspectContract(ctx, h, []string{a.Binary})
	if err != nil {
		return nil, err
	}
	if st.Mode == ModeDocker {
		if o.Image == "" {
			return nil, terminalError("install.error.image_identity")
		}
		if !o.LocalImage {
			if err := runQuiet(ctx, h, []string{"docker", "pull", o.Image}, longTimeout); err != nil {
				return nil, err
			}
		}
		raw, err := h.Output(ctx, []string{"docker", "image", "inspect", "--format", "{{.Id}}", o.Image}, 30*time.Second)
		if err != nil {
			return nil, err
		}
		a.Image = strings.TrimSpace(raw)
		if !imageID(a.Image) {
			return nil, terminalError("install.error.image_identity")
		}
		contract, err := inspectContract(ctx, h, []string{"docker", "run", "--rm", "--network", "none", "--entrypoint", BinPath, a.Image})
		if err != nil {
			return nil, err
		}
		if contract != a.Contract {
			return nil, terminalError("install.error.contract")
		}
		sum, err := h.Output(ctx, []string{"docker", "run", "--rm", "--network", "none", "--entrypoint", "sha256sum", a.Image, BinPath}, 30*time.Second)
		if err != nil {
			return nil, err
		}
		fields := strings.Fields(sum)
		if len(fields) != 2 || fields[0] != digest || fields[1] != BinPath {
			return nil, terminalError("install.error.shim")
		}
		b, err := h.ReadFile(ComposePth)
		if err != nil {
			return nil, err
		}
		b, err = composeImage(b, a.Image)
		if err != nil {
			return nil, err
		}
		a.Compose = dir + "/compose.yaml"
		if err = atomicWrite(h, a.Compose, b, 0600); err != nil {
			return nil, err
		}
	} else {
		plan, err := installedPlan(h, st)
		if err != nil {
			return nil, err
		}
		a.Unit = dir + "/wg-guard.service"
		if err = atomicWrite(h, a.Unit, []byte(RenderUnit(plan)), 0o600); err != nil {
			return nil, err
		}
	}
	return a, nil
}
func imageID(s string) bool {
	return strings.HasPrefix(s, "sha256:") && hexLength(strings.TrimPrefix(s, "sha256:"), 64)
}
func composeImage(b []byte, image string) ([]byte, error) {
	lines := strings.Split(string(b), "\n")
	n := 0
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "image:") {
			n++
			indent := len(line) - len(strings.TrimLeft(line, " \t"))
			lines[i] = line[:indent] + "image: " + image
		}
	}
	if n != 1 {
		return nil, terminalError("install.error.compose")
	}
	return ensureComposeLogPolicy(lines)
}

func ensureComposeLogPolicy(lines []string) ([]byte, error) {
	serviceStart, serviceEnd := -1, len(lines)
	for index, line := range lines {
		if line == "  wg-guard:" {
			if serviceStart >= 0 {
				return nil, terminalError("install.error.compose")
			}
			serviceStart = index
			continue
		}
		if serviceStart >= 0 && strings.TrimSpace(line) != "" && len(line)-len(strings.TrimLeft(line, " \t")) <= 2 {
			serviceEnd = index
			break
		}
	}
	if serviceStart < 0 {
		return nil, terminalError("install.error.compose")
	}
	start, end, insert := -1, -1, -1
	for index := serviceStart + 1; index < serviceEnd; index++ {
		line := lines[index]
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		if indent == 4 && strings.TrimSpace(line) == "logging:" {
			if start >= 0 {
				return nil, terminalError("install.error.compose")
			}
			start = index
			end = index + 1
			for end < serviceEnd {
				next := lines[end]
				nextIndent := len(next) - len(strings.TrimLeft(next, " \t"))
				if strings.TrimSpace(next) != "" && nextIndent <= 4 {
					break
				}
				end++
			}
		}
		if insert < 0 && indent == 4 && strings.TrimSpace(line) == "volumes:" {
			insert = index
		}
	}
	policy := strings.Split(strings.TrimSuffix(dockerLogPolicy, "\n"), "\n")
	if start >= 0 {
		lines = append(append(append([]string(nil), lines[:start]...), policy...), lines[end:]...)
		return []byte(strings.Join(lines, "\n")), nil
	}
	if insert < 0 {
		return nil, terminalError("install.error.compose")
	}
	lines = append(append(append([]string(nil), lines[:insert]...), policy...), lines[insert:]...)
	return []byte(strings.Join(lines, "\n")), nil
}
func deployArtifact(h Host, st *State, a *Artifact) error {
	if err := validateArtifact(a); err != nil {
		return err
	}
	digest, _, err := fileDigest(context.Background(), h, a.Binary, 256<<20)
	if err != nil {
		return err
	}
	if digest != a.BinarySHA256 {
		return terminalError("install.error.image.5")
	}
	if err = h.CopyFile(a.Binary, BinPath, 0755); err != nil {
		return err
	}
	if st.Mode == ModeDocker {
		b, err := h.ReadFile(a.Compose)
		if err != nil {
			return err
		}
		b, err = composeImage(b, a.Image)
		if err != nil {
			return err
		}
		return atomicWrite(h, ComposePth, b, 0644)
	}
	if a.Unit != "" {
		unit, err := h.ReadFile(a.Unit)
		if err != nil {
			return err
		}
		if err := atomicWrite(h, UnitPath, unit, 0o644); err != nil {
			return err
		}
		if strings.Contains(string(unit), "LogNamespace=wg-guard") {
			if err := h.MkdirAll(JournalRetentionDir, 0o755); err != nil {
				return err
			}
			if err := atomicWrite(h, JournalRetentionPath, []byte(RenderJournalRetention()), 0o644); err != nil {
				return err
			}
		} else if err := h.Remove(JournalRetentionPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}
func startService(ctx context.Context, h Host, st *State) error {
	if st.Mode == ModeDocker {
		return runQuiet(ctx, h, []string{"docker", "compose", "-f", ComposePth, "up", "-d", "--pull", "never"}, longTimeout)
	}
	if err := runQuiet(ctx, h, []string{"systemctl", "daemon-reload"}, 30*time.Second); err != nil {
		return err
	}
	if err := runQuiet(ctx, h, []string{"systemctl", "try-restart", "systemd-journald@wg-guard.service"}, 30*time.Second); err != nil {
		return err
	}
	return runQuiet(ctx, h, []string{"systemctl", "restart", "wg-guard"}, 90*time.Second)
}
func stopService(ctx context.Context, h Host, st *State) error {
	if st.Mode == ModeDocker {
		if _, err := h.Stat(ComposePth); !errors.Is(err, fs.ErrNotExist) {
			if err != nil {
				return err
			}
			if err := runQuiet(ctx, h, []string{"docker", "compose", "-f", ComposePth, "down"}, longTimeout); err != nil {
				return err
			}
		}
		raw, err := h.Output(ctx, []string{"docker", "ps", "--filter", "name=^/" + Container + "$", "--format", "{{.ID}}"}, 30*time.Second)
		if err != nil {
			return err
		}
		if strings.TrimSpace(raw) != "" {
			return terminalError("install.error.stop")
		}
		return nil
	}
	_, err := stopNativeService(ctx, h)
	return err
}

type nativeUnitState struct{ load, active string }

func (s nativeUnitState) absent() bool  { return s.load == "not-found" && s.active == "inactive" }
func (s nativeUnitState) stopped() bool { return s.active == "inactive" || s.active == "failed" }

// File absence alone is insufficient: systemd can retain a loaded/running unit
// after its file was removed. Query both properties and reject incomplete or
// inconsistent observations, even if systemctl printed them before an error.
func inspectNativeUnit(ctx context.Context, h Host) (nativeUnitState, error) {
	raw, err := h.Output(ctx, []string{"systemctl", "show", "wg-guard.service", "--property=LoadState", "--property=ActiveState"}, 30*time.Second)
	if err != nil {
		return nativeUnitState{}, err
	}
	s := nativeUnitState{}
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || value == "" {
			return s, terminalError("install.error.stop")
		}
		switch key {
		case "LoadState":
			if s.load != "" {
				return s, terminalError("install.error.stop")
			}
			s.load = value
		case "ActiveState":
			if s.active != "" {
				return s, terminalError("install.error.stop")
			}
			s.active = value
		default:
			return s, terminalError("install.error.stop")
		}
	}
	switch s.load {
	case "loaded", "masked", "not-found":
	default:
		return s, terminalError("install.error.stop")
	}
	switch s.active {
	case "active", "reloading", "inactive", "failed", "activating", "deactivating", "maintenance":
	default:
		return s, terminalError("install.error.stop")
	}
	if s.load == "not-found" && !s.absent() {
		return s, terminalError("install.error.stop")
	}
	return s, nil
}

// The bool tells uninstall whether disable can be skipped for a confirmed
// absent unit. A genuine stop failure is never reclassified as absence.
func stopNativeService(ctx context.Context, h Host) (bool, error) {
	s, err := inspectNativeUnit(ctx, h)
	if err != nil {
		return false, err
	}
	if s.absent() {
		return true, nil
	}
	if err := runQuiet(ctx, h, []string{"systemctl", "stop", "wg-guard"}, 60*time.Second); err != nil {
		return false, err
	}
	s, err = inspectNativeUnit(ctx, h)
	if err != nil {
		return false, err
	}
	if !s.stopped() {
		return false, terminalError("install.error.stop")
	}
	return s.absent(), nil
}

// Recovery has its own deadline so cancellation cannot disable compensation.
func recoverTransaction(h Host, j *Journal, out io.Writer) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	fail := func(err error) error {
		if j.After != nil {
			j.After.Recovery = "recovery-required"
			_ = saveState(h, j.After)
		}
		return errors.Join(err, j.save(h, "recovery-required"), terminalError("install.error.recovery_failed"))
	}
	if j.Operation == "install" && j.After != nil {
		if j.DataMayHaveChanged {
			if err := stopService(ctx, h, j.After); err != nil {
				return fail(err)
			}
		}
		j.After.Recovery = "install-incomplete"
		if err := saveState(h, j.After); err != nil {
			return fail(err)
		}
		return errors.Join(j.save(h, "recovery-required"), terminalError("install.error.manual_recovery"))
	}
	if j.Operation != "update" && j.Operation != "rollback" {
		return pendingOperationError(j)
	}
	if j.Stage == "prepared" {
		return j.save(h, "aborted")
	}
	if j.Before == nil || j.Previous == nil {
		return fail(terminalError("install.error.journal"))
	}
	if err := stopService(ctx, h, j.Before); err != nil {
		return fail(err)
	}
	if j.DataMayHaveChanged && !dataCompatible(j.Previous, j.Candidate) {
		if j.Previous.Backup != nil {
			j.Previous.Backup.RestoreRequired = true
		}
		st := *j.Before
		st.Recovery = "restore-required"
		if err := saveState(h, &st); err != nil {
			return fail(err)
		}
		return errors.Join(j.save(h, "restore-required"), terminalError("install.error.restore_required"))
	}
	if err := deployArtifact(h, j.Before, j.Previous); err != nil {
		return fail(err)
	}
	if err := startService(ctx, h, j.Before); err != nil {
		return fail(err)
	}
	if err := waitHealthyRecorded(ctx, h, j.Before, updateHealthWindow, out); err != nil {
		return fail(err)
	}
	if err := saveState(h, j.Before); err != nil {
		return fail(err)
	}
	return j.save(h, "rolled-back")
}

func fileDigest(ctx context.Context, h Host, p string, limit int64) (string, bool, error) {
	if _, ok := h.(realHost); ok {
		if err := safeHostPath(p); err != nil {
			return "", false, err
		}
	}
	f, err := h.Open(p)
	if err != nil {
		return "", false, err
	}
	defer f.Close()
	hash := sha256.New()
	buf := make([]byte, 64<<10)
	var n int64
	encrypted := false
	for {
		if err := ctx.Err(); err != nil {
			return "", false, err
		}
		count, e := f.Read(buf)
		if n == 0 {
			encrypted = strings.HasPrefix(string(buf[:count]), "age-encryption.org/v1")
		}
		n += int64(count)
		if n > limit {
			return "", false, terminalError("install.error.archive")
		}
		hash.Write(buf[:count])
		if e == io.EOF {
			break
		}
		if e != nil {
			return "", false, e
		}
	}
	if n == 0 {
		return "", false, terminalError("install.error.archive")
	}
	return hex.EncodeToString(hash.Sum(nil)), encrypted, nil
}
func createBackup(ctx context.Context, h Host, st *State, id string) (*BackupIdentity, error) {
	dir := DataDir + "/backups/lifecycle-" + id
	if err := h.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	args := []string{BinPath}
	if st.Mode == ModeDocker {
		args = []string{"docker", "exec", Container, BinPath}
	}
	args = append(args, "backup", "create", "--reason", "pre-upgrade", "--output", dir)
	raw, err := h.Output(ctx, args, 5*time.Minute)
	if err != nil {
		return nil, err
	}
	// Never log complete delivery output: it can contain remote warnings.
	name := ""
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == "created" {
			if name != "" {
				return nil, terminalError("install.error.archive")
			}
			name = fields[1]
		}
	}
	if name == "" || path.Base(name) != name || strings.ContainsAny(name, "\\\r\n\t") || !strings.HasSuffix(name, ".wgg") {
		return nil, terminalError("install.error.archive")
	}
	b := &BackupIdentity{Path: dir + "/" + name}
	b.SHA256, b.Encrypted, err = fileDigest(ctx, h, b.Path, 8<<30)
	return b, err
}
func waitHealthyRecorded(ctx context.Context, h Host, st *State, within time.Duration, out io.Writer) error {
	cfg, err := ReadBootConfig(h, st.ConfigPath)
	if err != nil {
		return err
	}
	p := Plan{Mode: st.Mode, DataDir: st.DataDir, TLSMode: cfg.TLS.Mode, Domain: cfg.TLS.Domain, PanelPort: portOf(cfg.HTTPListen), ACMEHTTPPort: cfg.TLS.ACMEHTTPPort}
	if p.ACMEHTTPPort == 0 {
		p.ACMEHTTPPort = 80
	}
	return waitHealthy(ctx, h, p, within)
}
func imageFromCompose(content string) string {
	for _, line := range strings.Split(content, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "image:"); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}
