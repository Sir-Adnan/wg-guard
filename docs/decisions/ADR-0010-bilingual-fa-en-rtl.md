# ADR-0010 — Bilingual fa/en panel with full RTL (Persian default)

Status: accepted · Date: 2026-08-29

## Context

The audience includes Persian-speaking administrators. The product requires a bilingual panel —
Persian (default) and English — with full RTL support, without doubling the stylesheet or
adding a frontend framework.

## Decision

Server-side string tables in `internal/i18n` (embedded fa/en catalogs; key parity enforced by
tests); per-admin language preference; `<html lang dir>` set server-side. RTL via **CSS logical
properties** everywhere (one stylesheet serves both directions). Typography: **Vazirmatn**
(OFL, covers Latin + Arabic scripts) as unicode-range-split woff2 subsets. Data (IPs, keys,
counters) renders LTR with Latin digits and tabular numerals; fa dates use the Jalali calendar
via a small table-tested conversion package. Installer/CLI remain English. Themes:
light (default) / dark / system.

## Consequences

- One code path for both locales; RTL correctness is a CSS-authoring discipline (logical
  properties), verified in the visual QA gates at Phase 5.
- Jalali conversion is a self-contained, well-tested utility; no heavyweight date library.

## Alternatives rejected

- Mirrored duplicate stylesheet: two sources of truth, drift-prone.
- Client-side i18n runtime: heavier frontend and SEO/flash-of-wrong-language issues;
  server-side tables fit the server-rendered architecture.
