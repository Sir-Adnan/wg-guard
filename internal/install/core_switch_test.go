package install

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestCoreSwitchRequiresImpactConfirmation(t *testing.T) {
	h := installedFixture(t, ModeNative)
	if _, err := SwitchCore(context.Background(), h, CoreSwitchOptions{Selector: "recommended"}); err == nil {
		t.Fatal("unconfirmed impact accepted")
	}
}

func TestCoreRetryInterruptedStagesAndStateFailure(t *testing.T) {
	for _, stage := range []string{"prepared", "pending-reboot", "recovery-required"} {
		t.Run(stage, func(t *testing.T) {
			h := installedFixture(t, ModeNative)
			st, err := LoadState(h)
			if err != nil {
				t.Fatal(err)
			}
			j := &Journal{Schema: 1, ID: transactionID(), Operation: "core", Before: st, After: st}
			if err := j.save(h, stage); err != nil {
				t.Fatal(err)
			}
			if _, err := SwitchCore(context.Background(), h, CoreSwitchOptions{Selector: "recommended", ConfirmImpact: true}); err != nil {
				t.Fatal("core retry", err)
			}
			j, err = LoadJournal(h)
			if err != nil || j.Stage != "complete" {
				t.Fatal("core retry not completed", err)
			}
		})
	}
	h := &faultHost{memHost: installedFixture(t, ModeNative), failRename: StatePath}
	o := CoreSwitchOptions{Selector: "recommended", ConfirmImpact: true}
	if _, err := SwitchCore(context.Background(), h, o); err == nil {
		t.Fatal("state write failure hidden")
	}
	j, err := LoadJournal(h)
	if err != nil || j.Stage != "recovery-required" {
		t.Fatal("failure not recoverable", err)
	}
	if _, err := SwitchCore(context.Background(), h, o); err != nil {
		t.Fatal("state write repair cannot retry", err)
	}
}

type coreCommitFault struct {
	Host
	writes int
}

func (h *coreCommitFault) Rename(a, b string) error {
	if b == JournalPath {
		h.writes++
		if h.writes == 2 {
			return errors.New("synthetic journal commit failure")
		}
	}
	return h.Host.Rename(a, b)
}
func TestCoreRetryAfterFinalJournalWriteFailure(t *testing.T) {
	h := &coreCommitFault{Host: installedFixture(t, ModeNative)}
	o := CoreSwitchOptions{Selector: "recommended", ConfirmImpact: true}
	if _, err := SwitchCore(context.Background(), h, o); err == nil {
		t.Fatal("journal failure hidden")
	}
	if _, err := SwitchCore(context.Background(), h, o); err != nil {
		t.Fatal("journal retry failed", err)
	}
}

func TestCoreRetryNeverBypassesDifferentOperation(t *testing.T) {
	for _, operation := range []string{"restart", "restore", "update", "uninstall"} {
		t.Run(operation, func(t *testing.T) {
			h := installedFixture(t, ModeNative)
			st, err := LoadState(h)
			if err != nil {
				t.Fatal(err)
			}
			j := &Journal{Schema: 1, ID: transactionID(), Operation: operation, Before: st, After: st}
			if err := j.save(h, "prepared"); err != nil {
				t.Fatal(err)
			}
			before := string(h.files[JournalPath].data)
			if _, err := SwitchCore(context.Background(), h, CoreSwitchOptions{Selector: "recommended", ConfirmImpact: true}); err == nil || !strings.Contains(err.Error(), operation) {
				t.Fatal("wrong recovery guidance", err)
			}
			if string(h.files[JournalPath].data) != before {
				t.Fatal("different pending operation replaced")
			}
		})
	}
}
func TestCoreSwitchRejectsUnknownTransition(t *testing.T) {
	h := installedFixture(t, ModeNative)
	h.output["dpkg-query -W -f=${db:Status-Status}\t${Version} amneziawg-tools"] = "installed\tunknown"
	if _, err := SwitchCore(context.Background(), h, CoreSwitchOptions{Selector: "recommended", ConfirmImpact: true, Stdout: io.Discard}); err == nil {
		t.Fatal("unknown installed core accepted")
	}
	if h.ran("apt-get", "install") {
		t.Fatal("unknown transition installed packages")
	}
}
func TestCoreSwitchPreservesPendingReboot(t *testing.T) {
	h := installedFixture(t, ModeNative)
	h.files["/sys/module/amneziawg/srcversion"] = memFile{data: []byte("OLDLOADEDBUILD")}
	r, err := SwitchCore(context.Background(), h, CoreSwitchOptions{Selector: "recommended", ConfirmImpact: true, Stdout: io.Discard})
	if err == nil || !r.RebootRequired {
		t.Fatal("pending reboot hidden")
	}
	j, e := LoadJournal(h)
	if e != nil || j.Stage != "pending-reboot" {
		t.Fatalf("pending reboot not durable %v %+v", e, j)
	}
	if h.ran("modprobe", "-r") {
		t.Fatal("active module unloaded")
	}
}

func TestCoreRetryAfterObservationRecovers(t *testing.T) {
	h := installedFixture(t, ModeNative)
	previous := h.output["modinfo"]
	h.output["modinfo"] = ""
	options := CoreSwitchOptions{Selector: "recommended", ConfirmImpact: true, Stdout: io.Discard}
	if _, err := SwitchCore(context.Background(), h, options); err == nil {
		t.Fatal("unknown module accepted")
	}
	h.output["modinfo"] = previous
	if _, err := SwitchCore(context.Background(), h, options); err != nil {
		t.Fatal("repaired observation cannot complete core operation", err)
	}
	j, err := LoadJournal(h)
	if err != nil || j.Stage != "complete" {
		t.Fatal("core retry not committed", err)
	}
	st, err := LoadState(h)
	if err != nil || st.Recovery != "" || st.Core.ModuleIdentity != "matches-disk" {
		t.Fatal("state not repaired", err)
	}
}
