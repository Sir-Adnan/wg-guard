package install

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestReviewedDKMSCoversAlreadyInstalledBootKernel(t *testing.T) {
	h := newMemHost()
	b, _ := SelectCore("recommended")
	h.dirs["/lib/modules"] = true
	for _, kernel := range []string{"6.8.0-138-generic", "6.8.0-146-generic", "6.8.0-147-generic"} {
		h.dirs["/lib/modules/"+kernel] = true
		h.files["/boot/vmlinuz-"+kernel] = memFile{perm: 0644}
	}
	h.files["/lib/modules/6.8.0-146-generic/build/Makefile"] = memFile{perm: 0644}
	h.dkms[b.KernelDKMSVersion+"|6.8.0-138-generic"] = true
	if err := ensureInstalledKernelBuilds(context.Background(), h, "6.8.0-138-generic", b); err != nil {
		t.Fatal(err)
	}
	if !h.dkms[b.KernelDKMSVersion+"|6.8.0-146-generic"] || h.dkms[b.KernelDKMSVersion+"|6.8.0-147-generic"] {
		t.Fatal("next boot kernel not covered or kernel without headers selected")
	}
	for _, command := range h.commands {
		if len(command.argv) > 1 && command.argv[0] == "dkms" && command.argv[1] == "install" {
			if argumentAfter(command.argv, "-m") != "amneziawg" || argumentAfter(command.argv, "-v") != b.KernelDKMSVersion {
				t.Fatal("unrelated DKMS module was selected")
			}
		}
	}
}

func TestReviewedDKMSRefusesFailedFutureBuild(t *testing.T) {
	h := newMemHost()
	b, _ := SelectCore("recommended")
	h.dkms[b.KernelDKMSVersion+"|6.8.0-138-generic"] = true
	h.dirs["/lib/modules"], h.dirs["/lib/modules/6.8.0-146-generic"] = true, true
	h.files["/boot/vmlinuz-6.8.0-146-generic"] = memFile{perm: 0644}
	h.files["/lib/modules/6.8.0-146-generic/build/Makefile"] = memFile{perm: 0644}
	h.failCmd["dkms"] = fmt.Errorf("incompatible future headers")
	if err := ensureInstalledKernelBuilds(context.Background(), h, "6.8.0-138-generic", b); err == nil {
		t.Fatal("failed future build reported ready")
	}
}

func TestReviewedDKMSKernelInventoryIsBounded(t *testing.T) {
	h := newMemHost()
	b, _ := SelectCore("recommended")
	h.dirs["/lib/modules"] = true
	for index := 0; index < 33; index++ {
		h.dirs[fmt.Sprintf("/lib/modules/6.8.0-%d-generic", index)] = true
	}
	err := ensureInstalledKernelBuilds(context.Background(), h, "6.8.0-138-generic", b)
	if err == nil || !strings.Contains(err.Error(), "bound") {
		t.Fatal("unbounded installed kernel inventory accepted")
	}
}
