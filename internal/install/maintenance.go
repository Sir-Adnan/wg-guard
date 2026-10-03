package install

import (
	"context"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/distribution"
	"github.com/Sir-Adnan/wg-guard/internal/updatequeue"
)

// MaintenanceInventory projects privileged host observations into a bounded,
// secret-free report. The web process never opens install state or runs commands.
func MaintenanceInventory(ctx context.Context, h Host) (updatequeue.Inventory, error) {
	i := updatequeue.Inventory{Schema: updatequeue.Schema, ObservedAt: time.Now().UTC(), ModuleIdentity: "unknown", Userspace: "unknown", Bridge: "unknown"}
	st, err := LoadState(h)
	if err != nil {
		return i, err
	}
	if st != nil {
		i.Mode = string(st.Mode)
		i.PanelVersion = st.Version
		if st.Current != nil {
			i.PanelCommit = st.Current.Build.Commit
			i.Channel = st.Current.Build.Channel
		}
		if st.Previous != nil && st.Current != nil {
			i.RollbackAllowed = dataCompatible(st.Current, st.Previous)
		}
		r, e := InspectInstalledCore(ctx, h)
		if e == nil {
			i.Bundle = r.Requested.ID
			i.ToolsVersion = r.ToolsVersion
			i.ToolsLocation = r.ToolsLocation
			i.ModuleLoaded = r.ModuleLoaded
			i.ModuleVersion = r.LoadedVersion
			i.ModuleIdentity = r.ModuleIdentity
			i.DKMS = r.KernelDKMS
			i.RebootRequired = r.RebootRequired
		}
		argv := []string{BinPath, "userspace-check"}
		if st.Mode == ModeDocker {
			argv = []string{"docker", "exec", Container, "/usr/local/bin/wg-guard", "userspace-check"}
		}
		if raw, e := h.Output(ctx, argv, 10*time.Second); e == nil {
			switch strings.TrimSpace(raw) {
			case "verified":
				i.Userspace = "verified"
				i.UserspaceCommit = st.Core.Requested.UserspaceCommit
			case "absent":
				i.Userspace = "absent"
			}
		}
	}
	if manager, e := LoadManagerBuild(ctx, h); e == nil {
		i.ManagerVersion = manager.Version
		i.ManagerCommit = manager.Commit
	}
	if prepared, e := LoadPreparedMaintenanceBuild(ctx, h); e == nil {
		i.PreparedVersion = prepared.Version
	}
	if p, e := InspectPlatform(ctx, h); e == nil {
		i.OS = p.OS
		i.OSVersion = p.Version
		i.Architecture = p.Arch
		i.Kernel = p.Kernel
	}
	if raw, e := h.Output(ctx, []string{"systemctl", "is-active", "wg-guard-update.path"}, 10*time.Second); e == nil && strings.TrimSpace(raw) == "active" {
		i.Bridge = "active"
	}
	if raw, e := h.Output(ctx, []string{"df", "--output=avail", "-B1", DataDir}, 10*time.Second); e == nil {
		fields := strings.Fields(raw)
		if len(fields) == 2 {
			i.DiskFreeBytes, _ = strconv.ParseUint(fields[1], 10, 64)
		}
	}
	if j, e := LoadJournal(h); e != nil {
		i.Recovery = "unreadable"
	} else if j != nil {
		if !j.terminal() {
			i.Recovery = j.Stage
			i.RecoveryOperation = j.Operation
		}
		if j.Previous != nil && j.Previous.Backup != nil {
			b := j.Previous.Backup
			i.Backup = &updatequeue.RecoveryBackup{OperationID: j.ID, Name: path.Base(b.Path), SHA256: b.SHA256, Encrypted: b.Encrypted}
		}
	}
	return i, nil
}

// MaintenancePreflight prepares/inspects a verified artifact, never deploys it,
// resets data, builds DKMS or claims that a backup has already been made.
func MaintenancePreflight(ctx context.Context, h Host, input updatequeue.Input, build *distribution.Build) (updatequeue.Inventory, error) {
	i, err := MaintenanceInventory(ctx, h)
	if err != nil {
		return i, err
	}
	p := &updatequeue.Preflight{Input: input, CheckedAt: time.Now().UTC(), Ready: true}
	p.Input.ExpectedCommit = ""
	p.Input.ExpectedSHA256 = ""
	add := func(code, state string) {
		p.Checks = append(p.Checks, updatequeue.Check{Code: code, State: state})
		if state == "fail" || state == "unknown" {
			p.Ready = false
		}
	}
	if i.OS == "ubuntu" && i.Architecture == "amd64" {
		add("platform", "pass")
	} else {
		add("platform", "fail")
	}
	if err := CheckLifecycleReady(h); err == nil {
		add("journal", "pass")
	} else {
		add("journal", "fail")
	}
	if i.DiskFreeBytes == 0 {
		add("disk", "unknown")
	} else if i.DiskFreeBytes < 2<<30 {
		add("disk", "fail")
	} else {
		add("disk", "pass")
	}
	st, e := LoadState(h)
	if e != nil || st == nil {
		add("runtime", "fail")
	} else {
		tool := "systemctl"
		if st.Mode == ModeDocker {
			tool = "docker"
		}
		if _, e := h.LookPath(tool); e == nil {
			add("runtime", "pass")
		} else {
			add("runtime", "fail")
		}
		if input.Operation == updatequeue.OperationCore || input.Operation == updatequeue.OperationAll {
			if st.Core.ExternalModule {
				add("module", "warn")
			} else if i.ModuleIdentity == "matches-disk" {
				add("module", "pass")
			} else if i.RebootRequired {
				add("module", "fail")
			} else {
				add("module", "unknown")
			}
			if raw, e := h.Output(ctx, []string{"dpkg-query", "-W", "-f=${db:Status-Status}", "linux-headers-" + i.Kernel}, 10*time.Second); e == nil && strings.TrimSpace(raw) == "installed" {
				add("headers", "pass")
			} else {
				add("headers", "warn")
			}
		}
		if build != nil {
			p.Commit = build.Commit
			p.SHA256 = build.SHA256
			contract, e := inspectContract(ctx, h, []string{build.BinaryPath})
			if e != nil {
				add("candidate", "fail")
			} else {
				add("candidate", "pass")
				p.DataCompatible = dataCompatible(st.Current, &Artifact{Contract: contract})
				if p.DataCompatible {
					add("data", "pass")
				} else {
					add("data", "warn")
				}
			}
			add("backup", "warn")
		}
	}
	i.Preflight = p
	return i, nil
}
