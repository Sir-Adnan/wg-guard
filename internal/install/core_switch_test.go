package install

import (
	"context"
	"io"
	"testing"
)

func TestCoreSwitchRequiresImpactConfirmation(t *testing.T) {
	h := installedFixture(t, ModeNative)
	if _, err := SwitchCore(context.Background(), h, CoreSwitchOptions{Selector: "recommended"}); err == nil {
		t.Fatal("unconfirmed impact accepted")
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
