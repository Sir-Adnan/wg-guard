# Release-readiness roadmap design

Approved: 2026-08-31.

## Context

Phases 0–7 delivered the foundation, networking, accounting, API, panel, operations, and
deployment layers. The remaining specification combines protocol correctness, observability,
complete product redesign, production certification, and release engineering. Treating all of
that as one “Phase 8” would make independent testing, honest status, and coherent commits
unreliable.

## Decision

Use five sequential phases:

1. **Phase 8 — Audit & configuration integrity:** resolve the QR/client-config blockers and
   stabilize the data/API/form contracts that later UI work consumes.
2. **Phase 9 — Operational observability:** create the final bounded metrics and logging
   contracts before the dashboard and operational screens are visually rebuilt.
3. **Phase 10 — Product UI/UX redesign:** migrate every product surface as one coherent visual
   release rather than leaving a long-lived half-redesigned panel.
4. **Phase 11 — Production certification:** test the feature-frozen product under security,
   concurrency, load, recovery, networking, OS, architecture, and deployment conditions.
5. **Phase 12 — Release candidate:** freeze documentation/API/repository state and prove the
   release artifacts without publishing them.

## Why this boundary is preferred

A three-phase alternative (correctness, product, release) was rejected because each gate would
combine unrelated implementation and verification risks. A six-or-more-phase alternative was
rejected because separating design-system construction from page migration creates an
inconsistent intermediate product without yielding a meaningful release gate.

The chosen ordering prevents rework:

- range/config models stabilize before forms and interface screens are redesigned;
- telemetry stabilizes before dashboard visuals are finalized;
- security, compatibility, and performance measurements target feature-frozen behavior;
- release artifacts are created only after certification, so artifact drills do not repeat for
  normal feature changes.

## Architecture constraints

- Preserve the single Go process, one scheduler goroutine, SQLite WAL, and CLI-driven pinned
  AmneziaWG backend.
- Keep Go templates, HTMX, vanilla JavaScript, embedded assets, and server-rendered lightweight
  charts; no production SPA or Node.js runtime.
- Use bounded queues, histories, and log storage; no per-user/per-admin background goroutines or
  busy polling.
- Never guess upstream AWG behavior. Parser acceptance, kernel/userspace runtime support, and
  real client compatibility are distinct evidence levels.
- Never expose keys, tokens, passwords, raw client configs, webhook secrets, backup secrets, or
  supplied VPS credentials in logs, argv, fixtures, documentation, or commits.
- Preserve stable `/api/v1` machine identifiers when presentation wording changes; synchronize
  API/OpenAPI whenever behavior or models change.
- Maintain complete fa/en catalog parity, RTL-safe CSS logical properties, and LTR technical data.
- Do not advertise compatibility that lacks real-host evidence.

## Cross-phase audit model

Phase 8 creates a severity-ranked findings register. Each later phase updates it. Critical/high
findings move to the earliest dependency-safe phase; low-value ideas do not expand scope. A phase
cannot close while it owns an unresolved release blocker.

## Verification model

Every phase produces tests, documentation, coherent commits, and a verification report. Unit,
integration, browser, real-VPS, real-client, and compatibility evidence remain separately labeled.
Phase 12 stops with a publication-ready candidate. Final public tags, releases, and container
images require explicit project-owner approval.

## Authoritative artifacts

- `ROADMAP.md`: phase order, state, and exit gates.
- `docs/development/release-readiness.md`: requirement ownership, blockers, findings, matrix.
- `docs/development/phase8.md` through `phase12.md`: phase-specific milestones, verification,
  documentation, completion, and deferral gates.
- `docs/development/status.md`: only implemented and honestly verified state.
- Behavior-specific living docs: updated in the same change as implementation.
