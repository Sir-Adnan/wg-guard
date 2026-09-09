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

// SwitchCore moves only between exact catalogued bundles. The recorded current
// bundle must first match observation; unknown/manual installations still fail
// closed. Source/package preparation does not unload the active module: when
// disk and loaded identities differ, state is committed as pending-reboot.
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
	// Retrying any interrupted core stage is safe after fresh package/module
	// observation; a different pending operation must never be replaced.
	if pending != nil && !pending.terminal() && pending.Operation != "core" {
		return CoreReport{}, pendingOperationError(pending)
	}
	current, err := InspectInstalledCore(ctx, h)
	if err != nil {
		return current, err
	}
	recorded, selectErr := SelectCore(current.Requested.ID)
	if selectErr != nil || recorded != current.Requested {
		return current, terminalError("install.error.core_transition")
	}
	pendingCore := pending != nil && !pending.terminal() && pending.Operation == "core"
	if pendingCore && (pending.After == nil || pending.After.Core.Requested != b) {
		return current, pendingOperationError(pending)
	}
	if !pendingCore && !coreReportMatchesBundle(current, recorded) {
		return current, terminalError("install.error.core_transition")
	}
	before := *st
	after := *st
	after.Core.Requested = b
	after.Recovery = ""
	j := pending
	if !pendingCore {
		j = &Journal{Schema: 1, ID: transactionID(), Operation: "core", Before: &before, After: &after}
	}
	if err := j.save(h, "prepared"); err != nil {
		return current, err
	}
	r := current
	if recorded != b || !coreReportMatchesBundle(current, b) {
		p, planErr := InstalledPlan(h, st)
		if planErr != nil {
			return current, errors.Join(planErr, j.save(h, "recovery-required"))
		}
		platform, platformErr := InspectPlatform(ctx, h)
		if platformErr != nil {
			return current, errors.Join(platformErr, j.save(h, "recovery-required"))
		}
		r, err = EnsurePrerequisites(ctx, h, p, platform, b, PrerequisitesAuto, st.Core.ExternalModule, &after, o.Stdout)
		after.Core = r
		j.After = &after
		if err != nil && !r.RebootRequired {
			after.Recovery = "recovery-required"
			return r, errors.Join(err, saveState(h, &after), j.save(h, "recovery-required"))
		}
	}
	stage := "complete"
	if r.RebootRequired {
		stage = "pending-reboot"
		after.Recovery = stage
	} else if r.ModuleIdentity != "matches-disk" {
		stage = "recovery-required"
		after.Recovery = stage
	}
	if err := saveState(h, &after); err != nil {
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
