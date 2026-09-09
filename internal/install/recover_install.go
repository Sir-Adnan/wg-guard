package install

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
)

// CleanupIncompleteInstall closes an initial-install journal only when the
// durable transaction record proves that runtime/data changes never began.
// The persistent manager, verified build receipt, downloaded core cache and
// data directory deliberately remain available for an immediate retry.
func CleanupIncompleteInstall(_ context.Context, h Host, confirmed bool, out io.Writer) error {
	if !h.IsRoot() {
		return terminalError("install.error.root")
	}
	if !confirmed {
		return fmt.Errorf("install: incomplete setup cleanup requires confirmation")
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
	if j == nil || j.terminal() || j.Operation != "install" || j.Before != nil || j.After == nil || j.DataMayHaveChanged || j.PrerequisitesComplete || j.After.Recovery != "" && j.After.Recovery != "install-incomplete" {
		return fmt.Errorf("install: automatic cleanup is not safe for this lifecycle record")
	}
	st, err := LoadState(h)
	if err != nil {
		return err
	}
	if st != nil && (st.Recovery != "install-incomplete" || st.Mode != j.After.Mode || st.ConfigPath != j.After.ConfigPath || st.DataDir != j.After.DataDir) {
		return fmt.Errorf("install: incomplete setup state does not match its lifecycle record")
	}
	for _, artifact := range []string{ConfigPath, ComposePth, UnitPath} {
		if _, statErr := h.Stat(artifact); statErr == nil {
			return fmt.Errorf("install: deployment artifacts exist; use guided manual recovery")
		} else if !errors.Is(statErr, fs.ErrNotExist) {
			return statErr
		}
	}
	if err := h.Remove(StatePath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	j.After.Recovery = ""
	if err := j.save(h, "aborted"); err != nil {
		return err
	}
	if out != nil {
		fmt.Fprintln(out, "Incomplete setup cleared. The local manager, cached build and data were kept.")
	}
	return nil
}
