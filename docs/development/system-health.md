# System health follow-up

Implemented on 2026-10-05 as a source follow-up to operational Phase 19. It does
not complete Phase 20 host/client certification, owner export verification or
authorize a new release/rebuild. The owner reported that no off-host archive has
yet been taken; the preparation checkpoint remains open.

## Problem and result

`runtimeapply.Coordinator` recorded process-local requested/applied state, but no
production reader exposed its snapshot. A committed account/profile change could
fail runtime application and leave the operator with only transient failure feedback.
The current `/system` panel page and authenticated `GET /api/v1/node/status` now
share `internal/nodestatus` receipts for readiness, active/last runtime attempt,
accounting freshness and the latest telemetry observation.

The page is read-only and reuses semantic cards, badges, guidance, disclosure and
native refresh. No repair button, subprocess, additional collector, worker, queue,
database table or dependency was added. The only active read is the existing bounded
DB readiness probe; other observations are fixed process-local state. Shared code
composes the same result for web and REST; handlers do not call one another.

`node.read` is required before observation. Global status is denied to reseller-bound
tokens/sessions. The public health discovery stays version/liveness-only; readiness
keeps its established 200/503 gate. The new endpoint returns HTTP 200 for a valid
snapshot whose readiness can still be `not_ready` or `unavailable`, with `no-store`.
Missing observations carry null timestamps and explicit unavailable/unobserved state.
OpenAPI, embedded `/docs` and the living API contract describe the additive endpoint.

Process-local sequence counters reset on restart and are not durable DB revisions,
order results or synchronization tokens. Last completion and current pending/running
state remain distinct. In-flight and last-failed flags contain no raw errors. Existing
periodic repair remains the retry path; status reads do not trigger it. Accounting
retains timestamp provenance but only recent evidence can be current/failed. Telemetry
also marks a future timestamp `sample_stale` instead of implying fresh health.

The dashboard links to this workspace with the existing node-read permission and
its restore action now selects the Backups restore tab, fixing an anchor-only link
which no longer reached the editor after section navigation was introduced.
The UI contract's outdated planned-domain claim was reconciled with implemented
unreleased Phase 18 source and its separate physical acceptance boundary.

## Verification

Fresh local checks covered pending/canceled/failed runtime observation, unchanged
public liveness, permission checks before reads, invalid tenant denial, no-store,
explicit nulls/unavailability, accounting stale/future/recovery, telemetry future/
stale observation, and no collection/application during status reads. Actual fake-node
composition shows both surfaces receive the same observation and agree with `/readyz`.
The full ordinary Go suite passed on Windows Go 1.27, using applicable cache for
unchanged packages. Focused Linux Go 1.26 race passed for runtimeapply, nodestatus,
metrics, telemetry, API, web and serve; vet/build passed. Exact final main CI remains
separate from these local checks.

Chromium and WebKit each passed 64 fa/en light/dark 320/390/768/1440-px cells across
healthy, pending, running and unavailable states, expanded technical detail,
keyboard focus, read-only content and two native/no-JavaScript disclosure cells.
The discovered narrow-screen recovery-action overflow was corrected with wrapping
and minimum-width rules. Rendered mobile/desktop artifacts were inspected locally.
No fresh axe scan, physical browser, VPS networking/SSL/client, real host command or
post-restore restart is claimed by this fixture. Those remain Phase 20 gates.
