package install

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestUninstallEOFIsNotConsent(t *testing.T) {
	h := installedFixture(t, ModeNative)
	before := string(h.files[StatePath].data)
	if _, err := Uninstall(context.Background(), h, UninstallOptions{Stdin: strings.NewReader("uninstall")}); err == nil {
		t.Fatal("EOF accepted as consent")
	}
	if string(h.files[StatePath].data) != before {
		t.Fatal("EOF removed state")
	}
}

func TestCandidateOwnerGatePreservesPriorDataContract(t *testing.T) {
	old := CurrentContract()
	old.LocalOwner = false
	if CheckContract(old) == nil {
		t.Fatal("owner-unsafe candidate accepted")
	}
	if !dataCompatible(&Artifact{Contract: old}, &Artifact{Contract: CurrentContract()}) {
		t.Fatal("known prior schema lost")
	}
	h := installedFixture(t, ModeNative)
	b, _ := json.Marshal(old)
	h.output[BinPath+" installer-contract"] = string(b)
	prior, err := retainCurrent(context.Background(), h, mustState(t, h))
	if err != nil || prior.Contract.DataContract != "schema7-h-ranges-v1" {
		t.Fatalf("retained contract lost: %v %+v", err, prior)
	}
}
func mustState(t *testing.T, h Host) *State {
	t.Helper()
	s, e := LoadState(h)
	if e != nil {
		t.Fatal(e)
	}
	return s
}

func TestManagedRestartLockJournalAndRetry(t *testing.T) {
	for _, mode := range []Mode{ModeNative, ModeDocker} {
		h := installedFixture(t, mode)
		h.commands = nil
		unlock, _ := h.LockLifecycle()
		if err := Restart(context.Background(), h); err == nil {
			t.Fatal("restart ignored lock")
		}
		unlock()
		if err := Restart(context.Background(), h); err != nil {
			t.Fatal(err)
		}
		j, err := LoadJournal(h)
		if err != nil || j.Operation != "restart" || j.Stage != "complete" {
			t.Fatalf("restart journal: %v %+v", err, j)
		}
		j.Operation = "update"
		if err := j.save(h, "prepared"); err != nil {
			t.Fatal(err)
		}
		h.commands = nil
		if err := Restart(context.Background(), h); err == nil || len(h.commands) > 0 {
			t.Fatal("restart overwrote pending update")
		}
	}
}
