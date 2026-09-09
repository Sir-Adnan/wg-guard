package install

import (
	"context"
	"errors"
	"io"
)

// restartLocked restarts one already-validated installation while the caller
// owns the lifecycle lock. afterStop can durably record the transition before
// the service starts again.
func restartLocked(ctx context.Context, h Host, st *State, afterStop func() error) error {
	if err := stopService(ctx, h, st); err != nil {
		return err
	}
	if afterStop != nil {
		if err := afterStop(); err != nil {
			return err
		}
	}
	if err := startService(ctx, h, st); err != nil {
		return err
	}
	return waitHealthyRecorded(ctx, h, st, updateHealthWindow, io.Discard)
}

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
		return pendingOperationError(j)
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
	if err := restartLocked(ctx, h, st, func() error { return j.save(h, "started") }); err != nil {
		return err
	}
	return j.save(h, "complete")
}
