# ADR-0009 — Vanilla-JS frontend (no Alpine.js), HTMX for server-driven UI

Status: accepted · Date: 2026-08-29

## Context

The panel needs light interactivity (modals, dropdowns, toasts, copy buttons, confirms, theme
toggle, form guards, refresh control) on top of server-rendered HTML. The spec allows "minimal
Alpine.js where useful" but prefers zero dependencies when maintainable.

## Decision

Server-rendered `html/template` + HTMX partial swaps + hand-written vanilla ES modules
(~8–10 KB gz). No Alpine.js, no SPA framework, no chart/QR/icon runtime libraries.

## Consequences

- ~15 KB gz saved versus the original Alpine-based plan; one fewer runtime dependency and no
  declarative-magic state to debug; all behavior is explicit and reviewable.
- The interactive surface is small and stable, so vanilla code stays maintainable; if it ever
  grows past ~15 KB of app code, that is treated as a design smell to re-examine.

## Alternatives rejected

- Alpine.js: 15 KB gz runtime + hidden declarative state for needs vanilla covers cleanly.
- React/Vue/Next: explicitly forbidden by the spec and the resource budgets.

## Note — 2026-10-18

The size figures above are historical. App JavaScript is now about 36 KB gzip across its
modules (HTMX excluded), and the project has no numerical asset-size targets (see the
[UI/UX contract](../product/ui-ux.md#settings-and-performance)): `bash scripts/check-assets.sh`
reports raw/gzip sizes instead. The decision itself (vanilla modules, no Alpine.js or SPA
framework) is unchanged.
