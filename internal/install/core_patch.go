package install

import (
	"crypto/sha256"
	"fmt"
	"path"
	"strings"
)

// kernelSourcePatch is a reviewed compile-compatibility correction applied to
// a bundle's DKMS source copy. The pinned upstream checkout stays pristine;
// both the original and the corrected file are pinned by SHA-256, so a changed
// upstream file or an incomplete edit fails closed before DKMS registration.
type kernelSourcePatch struct {
	file           string // relative to the DKMS source root
	originalSHA256 string
	patchedSHA256  string
	old, new       string
}

// Ubuntu 7.0.0-38 (26.04 GA and the 24.04 HWE kernel) backports the 7.1.5
// struct sock * signature of setup_udp_tunnel_sock() without changing
// LINUX_VERSION_CODE, so v3.1.20260906's version gate passes the wrong type
// (amneziawg-linux-kernel-module issue 259). The correction keeps that gate and
// selects each argument from the declared signature, as upstream PR 265
// proposes; earlier kernels compile to the same call as before.
var udpTunnelSignaturePatch = kernelSourcePatch{
	file:           "compat/compat.h",
	originalSHA256: "b14346040ce0188c47e2db2baad1a4f21aa784510f6c95bbb4aa58d5bbe691c9",
	patchedSHA256:  "b1331ce64d9c0a9ab3e2b896ecb07798e7d562373f668eb3a094f323c1546a11",
	old: `#if LINUX_VERSION_CODE < KERNEL_VERSION(7, 1, 5)
#include <net/udp_tunnel.h>
#define setup_udp_tunnel_sock(net, sk, sock_cfg) setup_udp_tunnel_sock(net, sk->sk_socket, sock_cfg)
#define udp_tunnel_sock_release(sk) udp_tunnel_sock_release(sk->sk_socket)
#endif
`,
	new: `#if LINUX_VERSION_CODE < KERNEL_VERSION(7, 1, 5)
#include <net/udp_tunnel.h>
/* WG-Guard: distributions backport the 7.1.5 struct sock * signatures without
 * changing LINUX_VERSION_CODE (Ubuntu 7.0.0-38 for setup_udp_tunnel_sock), so
 * select each argument from the declared signature (upstream issue 259).
 */
static inline void __compat_setup_udp_tunnel_sock(struct net *net, struct sock *sk,
						  struct udp_tunnel_sock_cfg *sock_cfg)
{
	setup_udp_tunnel_sock(net, __builtin_choose_expr(
		__builtin_types_compatible_p(typeof(&setup_udp_tunnel_sock),
			void (*)(struct net *, struct sock *, struct udp_tunnel_sock_cfg *)),
		sk, sk->sk_socket), sock_cfg);
}
#define setup_udp_tunnel_sock(net, sk, sock_cfg) __compat_setup_udp_tunnel_sock((net), (sk), (sock_cfg))
static inline void __compat_udp_tunnel_sock_release(struct sock *sk)
{
	udp_tunnel_sock_release(__builtin_choose_expr(
		__builtin_types_compatible_p(typeof(&udp_tunnel_sock_release), void (*)(struct sock *)),
		sk, sk->sk_socket));
}
#define udp_tunnel_sock_release(sk) __compat_udp_tunnel_sock_release(sk)
#endif
`,
}

// kernelSourcePatches maps a reviewed bundle ID to its corrections. Bundles
// without an entry build the pinned upstream source unchanged.
var kernelSourcePatches = map[string][]kernelSourcePatch{
	"awg-2026-10": {udpTunnelSignaturePatch},
}

func applyKernelSourcePatches(h Host, b CoreBundle, dkmsSource string) error {
	for _, p := range kernelSourcePatches[b.ID] {
		target := path.Join(dkmsSource, p.file)
		raw, err := h.ReadFile(target)
		if err != nil {
			return fmt.Errorf("install: read reviewed AWG source for correction: %w", err)
		}
		if fmt.Sprintf("%x", sha256.Sum256(raw)) != p.originalSHA256 || strings.Count(string(raw), p.old) != 1 {
			return fmt.Errorf("install: reviewed AWG source differs from the corrected revision")
		}
		patched := []byte(strings.Replace(string(raw), p.old, p.new, 1))
		if fmt.Sprintf("%x", sha256.Sum256(patched)) != p.patchedSHA256 {
			return fmt.Errorf("install: reviewed AWG source correction is incomplete")
		}
		if err := h.WriteFile(target, patched, 0o644); err != nil {
			return err
		}
	}
	return nil
}
