package install

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/Sir-Adnan/wg-guard/internal/distribution"
	"io/fs"
	"path"
	"strings"
)

const JournalPath = EtcDir + "/lifecycle.json"
const ArtifactDir = EtcDir + "/lifecycle"

type BackupIdentity struct {
	Path            string `json:"path"`
	SHA256          string `json:"sha256"`
	Encrypted       bool   `json:"encrypted"`
	RestoreRequired bool   `json:"restore_required"`
}
type Artifact struct {
	Build        distribution.Build `json:"build"`
	Image        string             `json:"image,omitempty"`
	Binary       string             `json:"binary"`
	BinarySHA256 string             `json:"binary_sha256"`
	Compose      string             `json:"compose,omitempty"`
	Contract     Contract           `json:"contract"`
	Backup       *BackupIdentity    `json:"backup,omitempty"`
}
type Journal struct {
	PackageIntents     []string  `json:"package_intents,omitempty"`
	RepositoryIntent   bool      `json:"repository_intent,omitempty"`
	Schema             int       `json:"schema"`
	ID                 string    `json:"id"`
	Operation          string    `json:"operation"`
	Stage              string    `json:"stage"`
	Before             *State    `json:"before,omitempty"`
	After              *State    `json:"after,omitempty"`
	Previous           *Artifact `json:"previous,omitempty"`
	Candidate          *Artifact `json:"candidate,omitempty"`
	DataMayHaveChanged bool      `json:"data_may_have_changed"`
}

func transactionID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func (j *Journal) save(h Host, stage string) error {
	previous := j.Stage
	j.Stage = stage
	if err := writeJSON(h, JournalPath, j); err != nil {
		j.Stage = previous
		return err
	}
	return nil
}
func (j *Journal) terminal() bool {
	return j.Stage == "complete" || j.Stage == "rolled-back" || j.Stage == "aborted"
}
func LoadJournal(h Host) (*Journal, error) {
	if _, ok := h.(realHost); ok {
		if err := safeHostPath(JournalPath); err != nil {
			return nil, err
		}
	}
	b, err := readRecord(h, JournalPath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(b) > 256<<10 {
		return nil, terminalError("install.error.journal")
	}
	var j Journal
	if json.Unmarshal(b, &j) != nil || j.Schema != 1 || !hexLength(j.ID, 32) {
		return nil, terminalError("install.error.journal")
	}
	switch j.Operation {
	case "install", "update", "rollback", "uninstall", "core", "restart":
	default:
		return nil, terminalError("install.error.journal")
	}
	switch j.Stage {
	case "prepared", "swap-pending", "started", "complete", "rolled-back", "aborted", "recovery-required", "restore-required", "prerequisites", "pending-reboot":
	default:
		return nil, terminalError("install.error.journal")
	}
	for _, s := range []*State{j.Before, j.After} {
		if s != nil {
			if err := validateState(s); err != nil {
				return nil, err
			}
		}
	}
	for _, a := range []*Artifact{j.Previous, j.Candidate} {
		if err := validateArtifact(a); err != nil {
			return nil, err
		}
	}
	return &j, nil
}
func artifactPath(p string) bool {
	return path.Dir(path.Dir(p)) == ArtifactDir && hexLength(path.Base(path.Dir(p)), 32) && (path.Base(p) == "binary" || path.Base(p) == "compose.yaml")
}
func validateArtifact(a *Artifact) error {
	if a == nil {
		return nil
	}
	if !artifactPath(a.Binary) || !hexLength(a.BinarySHA256, 64) || a.Compose != "" && !artifactPath(a.Compose) {
		return terminalError("install.error.state")
	}
	if a.Image != "" && (!strings.HasPrefix(a.Image, "sha256:") || !hexLength(strings.TrimPrefix(a.Image, "sha256:"), 64)) {
		return terminalError("install.error.state")
	}
	if a.Backup != nil {
		b := a.Backup
		if path.Dir(path.Dir(b.Path)) != DataDir+"/backups" || !strings.HasPrefix(path.Base(path.Dir(b.Path)), "lifecycle-") || !hexLength(strings.TrimPrefix(path.Base(path.Dir(b.Path)), "lifecycle-"), 32) || path.Base(b.Path) != strings.TrimSpace(path.Base(b.Path)) || !strings.HasSuffix(b.Path, ".wgg") || !hexLength(b.SHA256, 64) {
			return terminalError("install.error.state")
		}
	}
	return nil
}
func noPending(h Host) error {
	j, err := LoadJournal(h)
	if err != nil {
		return err
	}
	if j != nil && !j.terminal() {
		return terminalError("install.error.pending")
	}
	return nil
}
