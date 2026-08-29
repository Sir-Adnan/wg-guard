# UI/UX design system

The panel must look and feel like a paid product designed by a senior product designer — while
staying dramatically lighter than typical web apps. Premium comes from typography, spacing,
hierarchy, consistency, and micro-interactions; never from heavy frameworks or decorative
effects. (Original requirements: archived spec §53, §57, §58.)

## Foundations

- **Server-rendered** `html/template` + **HTMX** partial swaps + **vanilla ES modules**
  (~8–10 KB gz). No SPA framework, no Alpine, no chart/QR/icon runtime libraries.
- **Design tokens** as CSS custom properties: spacing (4/8 px grid), radii, type scale,
  semantic colors, borders, shadows, motion, z-index, breakpoints. No arbitrary values in
  templates.
- **Themes: light (default) / dark / system** from one stylesheet via `data-theme` +
  `prefers-color-scheme`; both themes meet WCAG-AA contrast.
- **Icon system**: Lucide subset compiled to one embedded SVG sprite (~10–15 KB), consistent
  1.5 px stroke; icons support recognition, never decoration. No emoji/raster/icon fonts.

## Bilingual & RTL (fa default / en)

- Server-side string tables in `internal/i18n` (embedded catalogs); fa and en key parity is
  enforced by a test. Per-admin language preference; installer/CLI remain English.
- `<html lang="…" dir="…">` set server-side. **Full RTL via CSS logical properties**
  (`margin-inline-*`, `padding-inline-*`, `inset-inline-*`, `text-align: start`) — one
  stylesheet serves both directions; `[dir="rtl"]` overrides only where physically unavoidable.
- **Typography: Vazirmatn** (OFL; covers Latin + Arabic-script glyphs, so one self-hosted
  family), unicode-range-split woff2 subsets (latin / arabic), `font-display: swap`.
- Data is language-independent: IPs, keys, and counters render LTR with **Latin digits** and
  tabular numerals; fa locale formats dates in the **Jalali calendar** (internal conversion
  package, table-tested), en locale Gregorian.

## Layout

- **Desktop**: compact sidebar (Dashboard, Users, Plans, Interfaces, Administrators, API
  Tokens, Webhooks, Node, Audit Logs, Backup, Settings), header with page context and status,
  dense data tables with bulk selection. Designed for administrators managing many accounts —
  not a stretched mobile layout.
- **Mobile (intentionally designed, not shrunk)**: drawer navigation, table→card adaptation at
  one breakpoint (traffic bar, expiry, device count, overflow menu per user), bottom sheets for
  detail/actions, 44 px touch targets. Both experiences are first-class; one component system
  adapts across breakpoints (tested at 360/390/768/1366/1440 px).

## Components

Small internal component library with consistent states (hover/active/focus/disabled/loading/
error): Button, IconButton, Input, Select, Toggle, SearchInput, Badge, StatusBadge, Card,
MetricCard, Modal (`<dialog>`), Drawer, Dropdown, Tooltip, Toast, Tabs, Pagination, Table +
toolbar, EmptyState, Skeleton, ProgressBar, TrafficProgress, ConfirmDialog, Avatar/InitialBadge.
Forms: clear labels, inline + server validation, logical grouping, Advanced options collapsed by
default (low-level AWG parameters never surface casually).

## Interaction & feedback

- Motion: 120–240 ms, `transform`/`opacity` only, `prefers-reduced-motion` honored; nothing
  animates while idle (zero idle CPU from the UI).
- Confirmation dialogs identify the target and consequence ("Delete 42 users? Their active VPN
  peers will be revoked."); destructive actions visually separated.
- Toasts for routine success; errors explain what failed, what to do, whether anything changed;
  no stack traces.
- Loading: skeletons for regions, button-level busy states, no double submissions.
- Empty states are actionable ("Create your first VPN account…").
- Copy-to-clipboard with feedback; consistent relative time; no layout shift; stable loading.

## Dashboard

Operational, not decorative: user counters (total/active/online/expired/traffic-exceeded/
expiring-soon), total traffic, node + AWG status, CPU/RAM/disk/network. Charts are
server-rendered SVG from rollup tables; no continuous animation. Auto-refresh (30 s) pauses
when the tab is hidden (Page Visibility).

## Performance budgets (enforced in CI)

| Asset | Budget (gzip) |
|---|---|
| JavaScript total (HTMX + app) | ≤ 30 KB |
| CSS total | ≤ 25 KB |
| Fonts (per locale subsets) | ≤ 150 KB total |
| Per-page HTML (typical list page) | ≤ 60 KB |

## Quality gates

No screen is done when it renders. Every screen is reviewed at desktop/tablet/phone and in
normal/empty/loading/error/success states; UX consistency rule: the same operation behaves
identically everywhere; final visual QA per screen before phase sign-off.
