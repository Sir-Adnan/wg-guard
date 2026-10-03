package updatequeue

import (
	"encoding/json"
	"errors"
	"io/fs"
	"path/filepath"
	"time"
)

// Inventory is the host's allowlisted view, never its privileged install state.
// No command, arbitrary file path, key, endpoint or raw process output belongs here.
type Inventory struct {
	PreparedVersion   string          `json:"prepared_version,omitempty"`
	Schema            int             `json:"schema"`
	ObservedAt        time.Time       `json:"observed_at"`
	Mode              string          `json:"mode"`
	PanelVersion      string          `json:"panel_version,omitempty"`
	PanelCommit       string          `json:"panel_commit,omitempty"`
	ManagerVersion    string          `json:"manager_version,omitempty"`
	ManagerCommit     string          `json:"manager_commit,omitempty"`
	Channel           string          `json:"channel,omitempty"`
	OS                string          `json:"os,omitempty"`
	OSVersion         string          `json:"os_version,omitempty"`
	Architecture      string          `json:"architecture,omitempty"`
	Kernel            string          `json:"kernel,omitempty"`
	Bundle            string          `json:"bundle,omitempty"`
	ToolsVersion      string          `json:"tools_version,omitempty"`
	ToolsLocation     string          `json:"tools_location,omitempty"`
	ModuleLoaded      bool            `json:"module_loaded"`
	ModuleVersion     string          `json:"module_version,omitempty"`
	ModuleIdentity    string          `json:"module_identity"`
	DKMS              string          `json:"dkms,omitempty"`
	RebootRequired    bool            `json:"reboot_required"`
	Userspace         string          `json:"userspace"` // verified, absent, unknown
	UserspaceCommit   string          `json:"userspace_commit,omitempty"`
	Bridge            string          `json:"bridge"` // active, inactive, unknown
	DiskFreeBytes     uint64          `json:"disk_free_bytes,omitempty"`
	Recovery          string          `json:"recovery,omitempty"`
	RecoveryOperation string          `json:"recovery_operation,omitempty"`
	RollbackAllowed   bool            `json:"rollback_allowed"`
	Backup            *RecoveryBackup `json:"backup,omitempty"`
	Preflight         *Preflight      `json:"preflight,omitempty"`
}

type RecoveryBackup struct {
	OperationID string `json:"operation_id"`
	Name        string `json:"name"`
	SHA256      string `json:"sha256"`
	Encrypted   bool   `json:"encrypted"`
}

type Outcome struct {
	PanelVersion   string          `json:"panel_version,omitempty"`
	ManagerVersion string          `json:"manager_version,omitempty"`
	Bundle         string          `json:"bundle,omitempty"`
	Recovery       string          `json:"recovery,omitempty"`
	RebootRequired bool            `json:"reboot_required"`
	Backup         *RecoveryBackup `json:"backup,omitempty"`
}

type Check struct {
	Code  string `json:"code"`
	State string `json:"state"`
}
type Preflight struct {
	SHA256         string    `json:"sha256,omitempty"`
	Input          Input     `json:"input"`
	CheckedAt      time.Time `json:"checked_at"`
	Commit         string    `json:"commit,omitempty"`
	DataCompatible bool      `json:"data_compatible"`
	Checks         []Check   `json:"checks"`
	Ready          bool      `json:"ready"`
}

func (q *Queue) PublishInventory(i Inventory) error {
	if !validInventory(i) {
		return ErrInvalid
	}
	return writeJSONAtomic(filepath.Join(q.Dir, "update-inventory.json"), i, true)
}

func (q *Queue) Inventory() (Inventory, error) {
	raw, err := readBoundedRegular(filepath.Join(q.Dir, "update-inventory.json"), 32<<10)
	if errors.Is(err, fs.ErrNotExist) {
		return Inventory{}, nil
	}
	if err != nil {
		return Inventory{}, err
	}
	var i Inventory
	if json.Unmarshal(raw, &i) != nil || !validInventory(i) {
		return Inventory{}, ErrInvalid
	}
	return i, nil
}

func validInventory(i Inventory) bool {
	if i.Schema != Schema || i.ObservedAt.IsZero() {
		return false
	}
	for _, value := range []string{i.PreparedVersion, i.Mode, i.PanelVersion, i.PanelCommit, i.ManagerVersion, i.ManagerCommit, i.Channel, i.OS, i.OSVersion, i.Architecture, i.Kernel, i.Bundle, i.ToolsVersion, i.ToolsLocation, i.ModuleVersion, i.ModuleIdentity, i.DKMS, i.Userspace, i.UserspaceCommit, i.Bridge, i.Recovery, i.RecoveryOperation} {
		if !validActor(value) {
			return false
		}
	}
	if i.Backup != nil && (!safeRequestID.MatchString(i.Backup.OperationID) || !safeCatalogID.MatchString(i.Backup.Name) || len(i.Backup.SHA256) != 64) {
		return false
	}
	if i.Preflight != nil {
		if !validInput(i.Preflight.Input) || len(i.Preflight.Checks) > 12 {
			return false
		}
		for _, c := range i.Preflight.Checks {
			switch c.Code {
			case "platform", "journal", "disk", "runtime", "headers", "candidate", "data", "backup", "module":
			default:
				return false
			}
			if c.State != "pass" && c.State != "warn" && c.State != "fail" && c.State != "unknown" {
				return false
			}
		}
	}
	return true
}
