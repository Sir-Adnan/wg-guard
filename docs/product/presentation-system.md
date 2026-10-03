# Panel presentation primitives

The Go/HTMX panel keeps its embedded browser runtime. Common primitives follow
shadcn's [Field](https://ui.shadcn.com/docs/components/base/field),
[Tabs](https://ui.shadcn.com/docs/components/base/tabs) and
[Popover](https://ui.shadcn.com/docs/components/base/popover) composition and interaction
patterns without introducing React or a production package runner.

## Numerals

Migration 0016 adds `appearance_defaults.digits` and per-admin `appearance_digits`.
Values are `latin` and `persian`; an empty personal value inherits the installation
default. Latin is the fresh/upgrade default regardless of language. The owner changes
the installation default; signed-in accounts change only their own overrides. Preset,
mode and language remain independent. Settings links to Appearance, where both controls
are available. Preferences do not change API numerics, stored quotas, passwords, URLs
or client configurations.

`internal/i18n/digits.go` is the final display layer. Canonical formatters retain ASCII.
Web translations normalize hardcoded Persian/Arabic numeral literals by default; the
Persian option changes human counts, amounts, dates and durations. IP/CIDR examples,
profile names, versions and string arguments such as usernames remain canonical. Form
inputs, copied values and identifiers keep their original values. Calendar and glyph
choice are independent: Persian language uses Jalali and English uses Gregorian.

## Guidance and structure

`presentation.js` enhances field hints and explicitly marked guidance into details
controls beside their labels. Original content/description IDs remain available to
assistive technology. Hover/focus opens help, click/touch pins it, and Escape/outside
interaction closes it. Native popovers provide top-layer placement and viewport
clamping; details retains an inline fallback. Validation errors, status information and
operational warnings stay visible. HTMX swaps receive the same enhancement; there is
no untrusted HTML injection or idle polling.

Cleanup uses searchable dropdowns for data kinds, eligible status unions and owners,
with Select all, partial-selection states and collapsed selected summaries. Its date
basis uses a radio menu. Menus share the same top-layer controller, support touch,
arrow focus movement, outside dismissal and Escape, and preserve their native details
fallback. Changing presentation cannot grant additional deletion permissions.

The tab primitive registers existing panels, supports directional arrows and Home/End
with roving focus, and opens the panel containing a server validation error. Without
JavaScript all sections remain accessible. Hidden panels retain submitted input values.
Interface forms group General, Address pools and Profile; packet parameters and overflow
pool editing are collapsible. Shared CSS uses existing semantic tokens for restrained
headers, type, borders, controls and focus states across presets.

## Pool editor

The editor offers automatic suggestions, prepared /24–/20 CIDRs, manual input and
overflow selection. Suggestions show capacity and unavailable/reserved/current/full
states from a bounded read-only snapshot. `/interfaces/pools/suggestions` is an
authenticated panel route, not a REST extension. Saving still runs authoritative
transaction and host-route validation. A CIDR reserved by another profile cannot be
assigned even if it has no devices. A full current pool advances allocation to another
configured pool on that profile; all full returns `DEVICE_POOL_EXHAUSTED`. See
[address maintenance](../operations/cleanup.md).
