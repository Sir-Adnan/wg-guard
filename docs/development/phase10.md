# Phase 10 — Product UI/UX redesign

Status: **planned; not implemented**. Starts after Phase 9 stabilizes monitoring/logging contracts.

## Objective

Deliver one premium, accessible, shadcn-style product experience across every page, state,
locale, theme, input method, and supported viewport without adding a production SPA runtime.

## Scope and deliverables

- Complete route/state/content audit and one reusable server-rendered component system.
- Neutral white light mode and near-black dark mode; restrained semantic colors; consistent
  spacing, typography, radii, borders, shadows, icons, motion, focus, hover, active, disabled,
  loading, success, warning, error, and destructive states.
- Shell, sidebar, header/footer, navigation, login, onboarding, error and empty states.
- Dashboard, users, create/edit/detail, devices/config/QR, subscriptions, plans, interfaces,
  settings, backups/restore, admins, tokens, webhooks/deliveries, audit and operational screens.
- Intentional phone/tablet/desktop/large-screen layouts; mobile cards/sheets are not compressed
  desktop tables, and narrow forms do not stretch on ultrawide displays.
- Settings information architecture with simple normal paths and progressive disclosure for
  advanced functions.
- Human-readable localized API-token scope, admin permission, and webhook-event labels and
  descriptions while stable API identifiers remain unchanged.
- Full English/Persian terminology, grammar, key parity, RTL/LTR, accessibility and interaction
  audit; technical values remain LTR with Latin digits.

## Milestones

1. Inventory routes, components, states, workflows, information architecture, and reference QA.
2. Establish tokens, typography, Lucide sprite, primitives, shell, themes, and responsive tiers.
3. Migrate auth/onboarding/dashboard and primary users/devices/config/subscription workflows.
4. Migrate plans/interfaces and their advanced AWG configuration experience.
5. Migrate settings, backup/restore, admins, tokens, webhooks/deliveries, audit and errors.
6. Complete localization, accessibility, responsive, state, performance and browser QA.

## Verification

- Handler/template/form/permission tests and fa/en catalog/key/raw-leak tests.
- Interaction tests for dialogs, drawers, sheets, menus, calendars, filters, pagination, copy,
  loading and destructive confirmations.
- Contrast, keyboard, focus, reduced-motion, touch-target and technical-data-direction checks.
- Browser matrix: approximately 320, 360, 390, 430, tablet, 1024, 1280, 1440, 1920, 2560 and
  ultrawide; fa/en × light/dark on representative normal/empty/error/loading states.
- Asset and typical-page HTML budgets remain measured; justified changes update budgets rather
  than silently weakening product quality.

## Real deployment QA

Run the full route/workflow matrix against the live TLS deployment, including real QR/config,
metrics, backups, permissions, menus, forms, touch, keyboard and viewport geometry. Use a real
phone pass where available and label any unavailable hardware honestly.

## Documentation

Update product UI/UX and requirements, architecture/project structure, testing/status/release
tracker, screenshots where maintained, API/OpenAPI wording, CHANGELOG and third-party notices.

## Completion criteria

RB-006 closes: every route belongs to one visual system, no legacy page/raw key/horizontal
overflow remains, critical workflows work in both locales/themes on mobile and desktop, and
browser/accessibility/asset gates pass.

## Deferred to Phase 11

Security certification, long soak, production load, recovery drills and broad compatibility
matrix. Release packaging remains Phase 12.
