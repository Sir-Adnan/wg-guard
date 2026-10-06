package install

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestKernelSourcePatchCorrectsPinnedUpstreamFile(t *testing.T) {
	original, err := os.ReadFile("testdata/amneziawg-compat-4569c4c.h")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := SelectCore("awg-2026-10")
	h := newMemHost()
	h.files["/usr/src/x/compat/compat.h"] = memFile{data: original, perm: 0o644}
	if err := applyKernelSourcePatches(h, b, "/usr/src/x"); err != nil {
		t.Fatal(err)
	}
	patched := h.files["/usr/src/x/compat/compat.h"].data
	if got := fmt.Sprintf("%x", sha256.Sum256(patched)); got != udpTunnelSignaturePatch.patchedSHA256 {
		t.Fatalf("patched digest = %s", got)
	}
	if !strings.Contains(string(patched), "__builtin_types_compatible_p(typeof(&setup_udp_tunnel_sock)") ||
		strings.Contains(string(patched), "setup_udp_tunnel_sock(net, sk->sk_socket, sock_cfg)") {
		t.Fatal("signature-selected call did not replace the version-gated macro")
	}

	// The uncorrected bundle keeps the pinned upstream bytes.
	previous, _ := SelectCore("awg-2026-09")
	h.files["/usr/src/y/compat/compat.h"] = memFile{data: original, perm: 0o644}
	if err := applyKernelSourcePatches(h, previous, "/usr/src/y"); err != nil || string(h.files["/usr/src/y/compat/compat.h"].data) != string(original) {
		t.Fatalf("awg-2026-09 source changed: %v", err)
	}
}

func TestKernelSourcePatchRefusesChangedUpstreamFile(t *testing.T) {
	original, err := os.ReadFile("testdata/amneziawg-compat-4569c4c.h")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := SelectCore("awg-2026-10")
	for name, body := range map[string][]byte{
		"altered":     append(append([]byte(nil), original...), '\n'),
		"already-new": []byte(strings.Replace(string(original), udpTunnelSignaturePatch.old, udpTunnelSignaturePatch.new, 1)),
	} {
		h := newMemHost()
		h.files["/usr/src/x/compat/compat.h"] = memFile{data: body, perm: 0o644}
		if err := applyKernelSourcePatches(h, b, "/usr/src/x"); err == nil {
			t.Errorf("%s upstream file accepted", name)
		}
		if string(h.files["/usr/src/x/compat/compat.h"].data) != string(body) {
			t.Errorf("%s upstream file was modified after refusal", name)
		}
	}
	h := newMemHost()
	if err := applyKernelSourcePatches(h, b, "/usr/src/missing"); err == nil {
		t.Error("missing upstream file accepted")
	}
}

// registeredDKMSHost reports the listed DKMS versions as registered when
// queried without a kernel, as `dkms status -m -v` does on a real host.
type registeredDKMSHost struct {
	*memHost
	registered map[string]bool
}

func (h *registeredDKMSHost) Output(ctx context.Context, argv []string, timeout time.Duration) (string, error) {
	if len(argv) == 6 && argv[0] == "dkms" && argv[1] == "status" && h.registered[argv[5]] {
		h.commands = append(h.commands, memCmd{argv: argv})
		return "amneziawg/" + argv[5] + ", 7.0.0-30-generic, x86_64: built", nil
	}
	return h.memHost.Output(ctx, argv, timeout)
}

func TestCorrectedBundleRetiresOnlySupersededSourceModule(t *testing.T) {
	b, _ := SelectCore("recommended")
	previous, _ := SelectCore("awg-2026-09")
	h := &registeredDKMSHost{memHost: newMemHost(), registered: map[string]bool{previous.KernelDKMSVersion: true}}
	h.files[CoreCacheDir+"/awg-2026-09/kernel/.wg-guard-installed"] = memFile{data: []byte(previous.KernelCommit + "\n")}
	if err := ensurePinnedKernel(context.Background(), h, "7.0.0-38-generic", b); err != nil {
		t.Fatal(err)
	}
	if !h.ran("dkms", "install", "-m", "amneziawg", "-v", b.KernelDKMSVersion, "-k", "7.0.0-38-generic") {
		t.Fatalf("corrected module not installed: %v", h.ranCommands())
	}
	if !h.ran("dkms", "remove", "-m", "amneziawg", "-v", previous.KernelDKMSVersion, "--all") {
		t.Fatalf("superseded source registration retained: %v", h.ranCommands())
	}
	if h.ran("dkms", "remove", "-m", "amneziawg", "-v", b.KernelDKMSVersion, "--all") {
		t.Fatal("selected module was retired")
	}
	if _, ok := h.files[CoreCacheDir+"/awg-2026-09/kernel/.wg-guard-installed"]; ok {
		t.Fatal("superseded source cache retained")
	}
	installed, retired := -1, -1
	for i, argv := range h.ranCommands() {
		joined := strings.Join(argv, " ")
		if installed < 0 && strings.HasPrefix(joined, "dkms install -m amneziawg -v "+b.KernelDKMSVersion) {
			installed = i
		}
		if strings.HasPrefix(joined, "dkms remove -m amneziawg -v "+previous.KernelDKMSVersion) {
			retired = i
		}
	}
	if installed < 0 || retired < installed {
		t.Fatal("superseded module retired before the corrected module was installed")
	}
}
