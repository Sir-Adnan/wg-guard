package install

import (
	"context"
	"errors"
	"fmt"
	"github.com/Sir-Adnan/wg-guard/internal/i18n"
	"io"
)

type CoreSwitchOptions struct {
	Selector      string
	ConfirmImpact bool
	Stdout        io.Writer
}

// SwitchCore is the catalog-only maintenance entry. The current reviewed
// catalog contains a single bundle, so there is no verified package-to-package
// transition to execute. Unknown installations require a manual migration.
func SwitchCore(ctx context.Context, h Host, o CoreSwitchOptions) (CoreReport, error) {
	if !h.IsRoot() {
		return CoreReport{}, terminalError("install.error.root")
	}
	unlock, err := h.LockLifecycle()
	if err != nil {
		return CoreReport{}, err
	}
	defer unlock()
	if !o.ConfirmImpact {
		return CoreReport{}, terminalError("install.error.core_confirmation")
	}
	b, err := SelectCore(o.Selector)
	if err != nil {
		return CoreReport{}, err
	}
	st, err := LoadState(h)
	if err != nil {
		return CoreReport{}, err
	}
	if st == nil {
		return CoreReport{}, terminalError("install.error.no_state")
	}
	pending, err := LoadJournal(h)
	if err != nil {
		return CoreReport{}, err
	}
	if pending != nil && !pending.terminal() && !(pending.Operation == "core" && pending.Stage == "pending-reboot") {
		return CoreReport{}, terminalError("install.error.pending")
	}
	r, err := InspectInstalledCore(ctx, h)
	if err != nil {
		return r, err
	}
	r.Requested = b
	// The only allowed transition is reaffirming the exact verified installation.
	// A future catalog addition needs a reviewed compatibility/package policy.
	if r.ToolsPackage != b.ToolsPackage || r.KernelPackage != b.KernelPackage {
		return r, terminalError("install.error.core_transition")
	}
	before := *st
	st.Core = r
	st.Recovery = ""
	j := &Journal{Schema: 1, ID: transactionID(), Operation: "core", Before: &before, After: st}
	if err := j.save(h, "prepared"); err != nil {
		return r, err
	}
	stage := "complete"
	if r.RebootRequired {
		stage = "pending-reboot"
		st.Recovery = stage
	} else if r.ModuleIdentity != "matches-disk" {
		stage = "recovery-required"
		st.Recovery = stage
	}
	if err := saveState(h, st); err != nil {
		return r, errors.Join(err, j.save(h, "recovery-required"))
	}
	if err := j.save(h, stage); err != nil {
		return r, err
	}
	if stage != "complete" {
		if r.RebootRequired {
			return r, terminalError("install.error.core.16")
		}
		return r, terminalError("install.error.core.22")
	}
	if o.Stdout != nil {
		fmt.Fprintln(o.Stdout, i18n.T(i18n.En, "install.core.single_bundle"))
	}
	return r, nil
}
