# Runtime readiness and recovery correction — 2026-10-05

The owner moved from Ubuntu 26.04 kernel `7.0.0-30-generic` to `7.0.0-38-generic`.
DKMS had the reviewed `1.0.0-wgguard.20260906` build only for the earlier kernel.
Doctor found no loaded module and both recorded kernel interfaces absent. The
candidate and restored predecessor therefore failed `/readyz` with HTTP 503,
although the old panel and its liveness-based Docker health check responded.

Headers for the new kernel were already installed. The owner's subsequent build
log showed the `setup_udp_tunnel_sock` `struct socket *`/`struct sock *` mismatch
documented in [upstream issue 259](https://github.com/amnezia-vpn/amneziawg-linux-kernel-module/issues/259).
This is an observed failure on an unverified later Ubuntu kernel, not a reviewed
patch or an expanded support claim. The owner subsequently supplied a terminal log
after a one-time boot of `7.0.0-30-generic`: module loading passed, recorded recovery
closed as `rolled-back`, and a panel-only update from `e81e4a6` to exact
`31de1d587b52537f3f6564af9563fc8e84917f5c` created its pre-update archive and passed
updated-panel readiness in about two seconds before reporting `Update complete`.
No post-update physical client traffic or the corrective source below is certified
by that report. The predecessor doctor warning of 133 runtime peers versus 137
enabled devices is an eligibility-blind heuristic, not proof of lost subscriber data.
Rebuilding the same OS/kernel would reproduce the prerequisite problem. Rebuild
choices require a verified off-host archive. A one-time boot does not change the
permanent default for a later reboot.

## Source changes

- Normal panel updates prove current node readiness before backup, durable update
  stages or deployment changes. A broken predecessor cannot be used as a silent
  recovery baseline. Explicit rollback/recorded recovery remain admitted.
- A recovery that restores predecessor artifacts but then fails start/readiness
  retains that predecessor's installed identity with `recovery-required`. It keeps
  the journal pending and never mutates its Before/After snapshots to pretend success.
- CLI status reports readiness separately from liveness and TLS access, so a
  responding web panel cannot hide missing tunnel prerequisites.

Focused install/CLI tests and vet passed locally, including current-unready refusal
without deployment mutation and restored-predecessor identity on failed readiness.
Linux Go 1.26 race passed for install/CLI. Exact main CI and the corrective source's
host acceptance remain separate evidence gates.
Public API/OpenAPI, state/journal schemas and pinned core sources are unchanged.
