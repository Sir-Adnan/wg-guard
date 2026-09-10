Approved with the following Phase 10 design refinements.

The proposed SSR + HTMX architecture is approved. Keep WG-Guard lightweight and do not migrate to React, SPA, Tailwind, or a heavy frontend runtime merely to use shadcn/ui.

However, revise the visual direction before implementation.

## Visual direction

I want the entire Web Panel redesigned from the ground up as a premium, modern, minimal product.

Use **shadcn/ui as the primary design reference across the entire product**, including its:

- visual hierarchy
- spacing
- typography
- cards
- forms
- buttons
- tables
- sheets
- dialogs
- dropdowns
- popovers
- tabs
- badges
- alerts
- toasts
- skeletons
- empty states
- charts
- navigation
- settings patterns
- responsive behavior
- interaction states

Do not add React just to use shadcn components. Reproduce the strongest shadcn-style component system natively within WG-Guard's existing SSR + HTMX + minimal-JS architecture.

## Glass, gradients and premium depth

Do not completely prohibit glass effects, gradients, shadows or motion.

Use them **selectively and tastefully** where they improve visual quality.

I want subtle premium influences similar to modern Apple/iOS interfaces, without turning the application into a flashy glassmorphism design.

Use where appropriate:

- subtle translucent/glass surfaces
- soft backdrop blur
- low-contrast premium gradients
- layered surface depth
- refined shadows
- subtle borders/highlights
- elegant hover/focus/pressed states
- smooth transitions
- restrained micro-interactions

Glass and gradients should not be used everywhere.

Most of the application should remain clean, neutral and shadcn-like. Use these effects only where they meaningfully improve hierarchy, polish or premium feel.

Avoid:

- excessive blur
- strong gradients
- neon styling
- unnecessary glow
- excessive shadows
- distracting animation
- visual clutter

## What “Premium” Means

For WG-Guard, **premium does not simply mean visually attractive, flashy, or filled with effects**.

A premium product should feel intentionally designed by an experienced product design team.

Premium quality means:

- clear visual hierarchy, so the user immediately understands what matters and what to do next;
- precise spacing, alignment, sizing, rhythm, and layout;
- professional typography for headings, labels, body text, technical data, statuses, and actions;
- complete consistency across buttons, cards, forms, tables, dialogs, sheets, navigation, badges, charts, and other components;
- polished interaction states for hover, focus, pressed, selected, loading, disabled, success, warning, error, and destructive actions;
- subtle and purposeful micro-interactions rather than excessive animation;
- controlled visual depth using borders, shadows, glass, blur, gradients, highlights, and elevation only where they improve hierarchy and polish;
- excellent responsive behavior, with mobile and desktop intentionally designed for their own environments;
- complete loading, empty, error, success, unavailable, disabled, and destructive states;
- excellent readability for technical information such as IPs, CIDRs, ports, keys, traffic values, timestamps, charts, and system status;
- excellent accessibility, keyboard navigation, touch targets, focus states, contrast, and reduced-motion support;
- attention to small details so there are no awkward gaps, inconsistent spacing, broken dropdowns, raw API identifiers, accidental overflow, unclear actions, or unfinished-looking states.

Premium should come from **precision, consistency, usability, polish, and restraint**.

It should NOT mean:

- excessive gradients;
- glass everywhere;
- excessive blur;
- excessive shadows;
- constant animations;
- unnecessary hover movement;
- neon/glowing interfaces;
- visual clutter;
- sacrificing usability for decoration.

The desired overall direction is:

**shadcn-like precision + Apple/iOS-like polish + modern minimalism + excellent UX + consistent responsive behavior.**

Glass, gradients, motion, shadows, and other premium visual effects should enhance this foundation rather than replace it.

The final result should feel like a mature, commercially polished product built by a top-tier design and engineering team.

## Hover and interaction states

Every interactive component must implement all relevant states where applicable:

- default
- hover
- focus
- active/pressed
- selected
- disabled
- loading
- success
- warning
- error

Apply refined hover interactions to cards, menu items, rows, buttons, navigation items, dropdown entries and other interactive surfaces where appropriate.

Do not animate static/non-interactive containers just for decoration.

## Motion

Use smooth, premium animations and micro-interactions throughout the product where appropriate:

- sheets/drawers
- dialogs
- dropdowns
- popovers
- menus
- tabs
- collapsible advanced sections
- notifications/toasts
- loading transitions
- chart updates
- hover/selection states

Animations must remain fast, subtle and purposeful.

Respect `prefers-reduced-motion`.

## Icons and visual assets

Use high-quality SVG throughout the interface.

Prefer Lucide-style icons consistently.

Avoid inconsistent icon sets, raster UI icons or unnecessary decorative assets.

## Charts and dashboard

Redesign dashboard charts and graphs from scratch.

Use premium shadcn-style chart patterns for:

- CPU
- RAM
- network RX/TX
- VPN traffic
- active users
- active peers
- disk
- node/AWG health
- other genuinely useful operational information

Charts should be:

- clean
- responsive
- lightweight
- readable in Light and Dark themes
- RTL/LTR safe
- visually premium
- free from unnecessary clutter

## Complete redesign means COMPLETE

Do not interpret Phase 10 as incremental CSS cleanup.

Redesign every existing page, subpage, state and major component from the ground up while preserving proven backend behavior.

This includes at minimum:

- Login
- Authentication flows
- Onboarding
- Dashboard
- Users
- Create User
- Bulk User creation/actions
- User Detail
- Devices
- Config / QR
- Plans
- Interfaces
- Create Interface
- Edit Interface
- all AmneziaWG advanced settings
- Settings
- Backups
- Restore
- Admins
- API Tokens
- Webhooks
- Audit
- Subscription management
- public subscription page
- error pages
- empty/loading/error/success states
- dialogs
- confirmations
- forms
- tables
- mobile layouts

The public subscription/subscription-download experience must receive the same level of design quality as the authenticated admin panel.

No page should remain visually legacy.

## Desktop, mobile and all screen sizes

Mobile and desktop must each feel intentionally designed.

Do not treat mobile as a simplified desktop layout or desktop as an enlarged mobile layout.

Design and verify for:

- 320
- 360
- 390
- 430
- tablet
- 768
- 1024
- 1280
- 1440
- 1920
- 2560
- ultrawide

Use appropriate responsive transformations such as:

- table → cards where appropriate
- desktop sidebar → mobile navigation
- dialog → sheet/drawer where better on mobile
- adaptive spacing/density
- sensible max-width containers
- correct large-screen centering

No horizontal overflow.

## Themes

Light mode:

- primarily clean white
- neutral/zinc surfaces
- subtle depth

Dark mode:

- true near-black / `#09090b` direction
- refined elevated surfaces
- avoid washed-out gray dark themes

Do not use purple as the primary identity.

## Settings UX

Completely redesign Settings architecture and UX.

Keep the proposed grouped structure, but review every setting for:

- usefulness
- terminology
- defaults
- grouping
- duplication
- missing explanation
- confusing behavior
- destructive implications
- whether it belongs in Advanced

Scopes, Permissions and Webhook Events must use localized human-readable labels and concise descriptions rather than exposing raw identifiers as primary UI text.

## Performance budget

The current lightweight budget is important, but it must not prevent premium product quality.

If additional CSS, SVG, chart logic or minimal JavaScript is genuinely required for the new component system and premium UX, increase the existing asset budget reasonably.

Do not optimize for the smallest possible byte count at the expense of UX.

However:

- measure the resulting CSS/JS/HTML sizes;
- avoid unnecessary libraries;
- avoid duplicate assets;
- avoid runtime-heavy frontend frameworks;
- keep the final implementation efficient.

## Product/design ownership

Use your strongest senior product-design and frontend-engineering judgment.

Think like a top-tier:

- senior product designer
- lead UI/UX designer
- senior frontend engineer
- design-system architect

Do not mechanically reproduce my examples if a stronger solution exists.

Review the whole application holistically and implement the strongest professional solution.

The final result should feel like a mature premium commercial product, not merely an open-source admin dashboard.

Keep the code clean, modular, reusable, consistent and maintainable.

Update the Phase 10 design specification accordingly, then proceed with implementation.

## Execution efficiency

This is a large redesign, so execute it in coherent milestones rather than repeatedly running the entire QA matrix after every small UI change.

- Build and stabilize the shared design system/components first.
- Reuse those components across pages instead of creating page-specific duplicates.
- During implementation, run only targeted tests and browser checks for the area being changed.
- Run the full viewport × language × theme × state matrix once at the final Phase 10 QA gate, with milestone-level checks only where necessary.
- Do not repeatedly retest already-valid unrelated workflows unless affected code changed.
- Do not create unnecessary progress reports, review files, screenshots, fixtures, or verbose documentation.
- Keep permanent documentation concise and focused on lasting design-system, UX, accessibility, and architecture decisions.
- Do not rewrite proven backend/business logic merely because the frontend is being redesigned.

Premium quality is required, but avoid verification churn, documentation bloat, and unnecessary architecture changes.

## API / OpenAPI synchronization

If Phase 10 changes any user-visible API behavior, request/response schema, endpoint, field, validation rule, scope, permission, webhook event, or other public contract:

- update the REST API implementation as required;
- update `OpenAPI` at the same time;
- keep API examples and related documentation synchronized;
- add/update the relevant API/OpenAPI tests;
- preserve backward compatibility where practical;
- do not change API contracts only for visual/UI reasons.

If Phase 10 does not affect the API contract, do not modify the API/OpenAPI unnecessarily.

## Phase 10 Completion

When Phase 10 implementation is fully complete:

- run the final relevant tests and browser/UX verification;
- update all affected documentation to match the final implementation;
- update the README, ROADMAP, development status, Phase 10 documentation, UI/UX documentation, architecture notes, and any other relevant `.md` files;
- Do not create new documentation files unless the existing authoritative documentation cannot cleanly accommodate the required information.
- Keep permanent documentation concise.
- update API/OpenAPI documentation only if any visible API contract changed;
- remove or consolidate obsolete, duplicate, temporary, overly verbose, or low-value documentation created during development;
- keep permanent documentation concise, organized, accurate, and focused on important behavior, architecture, design decisions, limitations, operator guidance, and verified results;
- do not leave temporary progress logs, scratch notes, redundant review reports, or unnecessary documentation files in the final repository;
- ensure documentation does not claim anything that was not actually implemented or verified;
- keep CI green;
- commit the completed Phase 10 work coherently;
- merge the completed Phase 10 branch(es) into `main`;
- push the final `main` branch to GitHub;
- verify that `origin/main` contains the completed Phase 10 work;
- delete temporary local and remote Phase 10 branches after confirming the merge;
- leave the repository clean and synchronized.

Finally, provide me with a concise final Phase 10 completion report covering:

- what was redesigned and implemented;
- major UI/UX and design-system changes;
- important architectural decisions;
- responsive/mobile/desktop work completed;
- accessibility and localization work completed;
- tests and browser/device/viewport verification performed;
- performance and asset-size results;
- what was verified on the real VPS;
- any remaining limitations, deferred items, or unverified areas.

Do not start Phase 11 until Phase 10 is fully completed, merged into `main`, pushed, documented, and reported.
