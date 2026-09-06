package install

import (
	"context"
	"errors"
	"io"
)

// Restart shares lifecycle locking, stop/start and health validation. Interrupted
// restarts can be retried explicitly, but never replace another pending operation.
func Restart(ctx context.Context, h Host) (resultErr error) {
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
	if j != nil && !j.terminal() && j.Operation != "restart" {
		return terminalError("install.error.pending")
	}
	j = &Journal{Schema: 1, ID: transactionID(), Operation: "restart", Before: st, After: st}
	if err := j.save(h, "prepared"); err != nil {
		return err
	}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, j.save(h, "recovery-required"))
		}
	}()
	if err := stopService(ctx, h, st); err != nil {
		return err
	}
	if err := j.save(h, "started"); err != nil {
		return err
	}
	if err := startService(ctx, h, st); err != nil {
		return err
	}
	if err := waitHealthyRecorded(ctx, h, st, updateHealthWindow, io.Discard); err != nil {
		return err
	}
	return j.save(h, "complete")
}
