package install

import (
	"context"
	"errors"
	"github.com/Sir-Adnan/wg-guard/internal/backup"
	"io"
	"strings"
	"time"
)

// Prepare validates and obtains explicit consent without touching active data.
// A nonnil identity requires exact original-schema recovery from that archive.
// Apply executes only after the shared coordinator proves the service stopped.
type RestoreOptions struct {
	Recover bool
	Retry   bool
	Prepare func(context.Context, *BackupIdentity) (func(context.Context) error, error)
}

func Restore(ctx context.Context, h Host, o RestoreOptions) error {
	if !h.IsRoot() {
		return terminalError("manage.root")
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
	if st == nil {
		return terminalError("install.error.health.3")
	}
	j, err := LoadJournal(h)
	if err != nil {
		return err
	}
	var identity *BackupIdentity
	if o.Recover {
		if j == nil || j.terminal() || j.Before == nil || j.Previous == nil || j.Previous.Backup == nil || !j.DataMayHaveChanged || !j.Previous.Backup.RestoreRequired {
			return terminalError("install.error.restore_required")
		}
		identity = j.Previous.Backup
		digest, encrypted, err := fileDigest(ctx, h, identity.Path, 8<<30)
		if err != nil {
			return err
		}
		if digest != identity.SHA256 || encrypted != identity.Encrypted {
			return terminalError("install.error.archive")
		}
		digest, _, err = fileDigest(ctx, h, j.Previous.Binary, 256<<20)
		if err != nil {
			return err
		}
		if digest != j.Previous.BinarySHA256 {
			return terminalError("install.error.image.5")
		}
		if j.Before.Mode == ModeDocker {
			got, err := h.Output(ctx, []string{"docker", "image", "inspect", "--format", "{{.Id}}", j.Previous.Image}, 30*time.Second)
			if err != nil || strings.TrimSpace(got) != j.Previous.Image {
				return terminalError("install.error.image_identity")
			}
			got, err = h.Output(ctx, []string{"docker", "run", "--rm", "--network", "none", "--entrypoint", "sha256sum", j.Previous.Image, BinPath}, 30*time.Second)
			fields := strings.Fields(got)
			if err != nil || len(fields) != 2 || fields[0] != j.Previous.BinarySHA256 {
				return terminalError("install.error.image.5")
			}
		}
	} else if j != nil && !j.terminal() && !(o.Retry && j.Operation == "restore") {
		return terminalError("install.error.pending")
	}
	if o.Prepare == nil {
		return terminalError("install.error.restore_required")
	}
	apply, err := o.Prepare(ctx, identity)
	if err != nil {
		return err
	}
	if apply == nil {
		return terminalError("install.error.restore_required")
	}
	if !o.Recover {
		j = &Journal{Schema: 1, ID: transactionID(), Operation: "restore", Before: st, After: st}
		if err := j.save(h, "prepared"); err != nil {
			return err
		}
	}
	// Failures keep the service stopped and a durable operation blocking later
	// lifecycle changes. Original files remain recoverable in backup's transaction.
	guard := DataDir + "/" + backup.RestoreGuardName
	fail := func(e error) error {
		return errors.Join(e, atomicWrite(h, guard, []byte("restore requires offline recovery\n"), 0600), j.save(h, "recovery-required"))
	}
	if err := stopService(ctx, h, st); err != nil {
		return fail(err)
	}
	if err := j.save(h, "swap-pending"); err != nil {
		return fail(err)
	}
	if err := atomicWrite(h, guard, []byte("offline restore in progress\n"), 0600); err != nil {
		return fail(err)
	}
	if err := apply(ctx); err != nil {
		return fail(err)
	}
	j.DataMayHaveChanged = true
	target := st
	if o.Recover {
		target = j.Before
		if err := deployArtifact(h, target, j.Previous); err != nil {
			return fail(err)
		}
	}
	if err := j.save(h, "started"); err != nil {
		return fail(err)
	}
	if err := h.Remove(guard); err != nil {
		return fail(err)
	}
	// Once data is replaced, cancellation cannot cut off service supervision.
	startCtx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := startService(startCtx, h, target); err != nil {
		_ = stopService(startCtx, h, target)
		return fail(err)
	}
	if err := waitHealthyRecorded(startCtx, h, target, updateHealthWindow, io.Discard); err != nil {
		_ = stopService(startCtx, h, target)
		return fail(err)
	}
	next := *target
	next.Recovery = ""
	if err := saveState(h, &next); err != nil {
		_ = stopService(startCtx, h, target)
		return fail(err)
	}
	stage := "complete"
	if o.Recover {
		stage = "rolled-back"
	}
	if err := j.save(h, stage); err != nil {
		_ = stopService(startCtx, h, target)
		return fail(err)
	}
	return nil
}
