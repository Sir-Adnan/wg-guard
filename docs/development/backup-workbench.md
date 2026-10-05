# Backup workbench follow-up

Implemented on 2026-10-05 as a Phase 19 source follow-up. Current main remains
unreleased; no server mutation, rebuild, off-host owner export, registry image or
public release was performed. Physical target acceptance remains Phase 20.

## Result and boundaries

The session-authenticated panel separates archive collection, verification/restore,
schedule editing and delivery. Archive cursor pages retain at most limit+1 candidates;
the selected older archive is preserved without rendering an unbounded option list.
Native navigation and forms remain available without JavaScript. Source inventory,
provenance and technical details use shared semantic components and fa/en presentation.
Saved reports suppress duplicated import/selection forms.

Non-destructive verification expands to private disk on the data volume, returns a
safe source report/input SHA-256, and removes decrypted data on ordinary completion/
failure. Restore preparation redirects to a saved GET report; refresh and returning
to the page do not run KDF or create another preview. Four retained private reviews
are admitted; incomplete/older previews are explicitly manageable. GET reads bounded
metadata, while approval and offline apply recheck payloads and the DB/key pair.
Panel preparation/confirmation require an enabled source owner; general CLI recovery
stays available. Initial install reuses that validated owner inventory.

Approval, pending cancellation, offline apply and interrupted recovery serialize
through a nonblocking work-byte claim on the persistent data-volume inode. Cancellation
matches the displayed metadata digest; stale forms cannot remove a later request.
Archive/KDF inspection shares creation's crypto claim but opens no active pair.
Purge excludes private inspection/review and admission rechecks tombstones. Original
rotation/exclusive-data and paired-recovery invariants remain covered.

Web controllers split collection/page, restore and schedule responsibilities; engine
listing, review policy and work claims are separate modules, with one shared restore
report partial. Shared flash composition preserves workspace queries/anchors. Stale
forms/downloads safely report an unavailable backup engine after permission checks.
No scheduler, generic executor, production dependency or archive-schema change was
added. REST paths, bodies, scopes and OpenAPI remain unchanged under ADR-0007; API
documentation explicitly distinguishes the browser-only operations.

## Verification

Fresh local checks covered cached-report restart/refresh, payload tampering before
approval, exact SHA/time, wrong/missing encrypted password, interrupted verification,
secret-free metadata, no retained verification payload, ownerless direct confirmation,
retained/incomplete preview limits, stale pending cancellation, cursor ties and
concurrent archive addition/deletion, archive/review cross-process claims, purge
exclusion, CSRF/permission/unavailable-engine responses and return-section feedback.

The full ordinary Go suite passed with Go 1.27 on Windows, with applicable unchanged
packages reusing Go cache. A fresh web check passed after controller/flash changes;
unchanged API contract checks reused cache. Go vet and a build passed. Linux Go 1.26
race checks passed for backup, web, nodestate and CLI, including the existing recovery/
rotation/initial-install tests. Controller file movement does not change those tested
declarations; exact final main CI remains a separate gate. Asset measurement passed.

Chromium and WebKit each passed 80 fa/en Light/Dark cells at 320/390/768/1440 px over
archives, restore selection, saved review, schedule editing and delivery. The matrix
includes expanded technical report/overflow and native section-anchor keyboard
activation. Both also passed native/no-JavaScript cursor pagination, verification,
saved-review reload and explicit approval/conditional cancellation. WebKit's native
verification uses keyboard submission to avoid script-disabled pointer-stability
instrumentation; this does not enable the application scripts. No fresh axe scan,
physical browser/device, actual Docker restart/host restore, network or client result
is inferred from these fixtures. Prior Phase 19 artifact evidence is not a candidate
artifact identity for this changed source.
