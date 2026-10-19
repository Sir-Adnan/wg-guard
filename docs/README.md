# WG-Guard documentation

The living contracts for the Go/SQLite/HTMX AmneziaWG node panel. Read the few pages your task
touches; this map is a route, not a checklist. Code and tests must back every claimed behavior.

## Start here

| Need | Read |
|---|---|
| What is supported and how it was verified | [Status](development/status.md) · [release readiness](development/release-readiness.md) · [roadmap](../ROADMAP.md) |
| Install, update, back up or recover a node | [GitHub installation](operations/github-install.md) · [deployment](operations/deployment.md) · [backup and restore](operations/backup-restore.md) · [lifecycle recovery](operations/lifecycle-recovery.md) |
| Product behavior and the panel | [Requirements](product/requirements.md) · [UI/UX contract](product/ui-ux.md) · [design system](product/design-system.md) |
| Outbounds, balancers and routing rules | [Egress routing](architecture/egress-routing.md) · [client apps, AmneziaVPN keys and WARP](integrations/client-apps-and-warp.md) |
| Architecture or data change | [Overview](architecture/overview.md) · [project structure](architecture/project-structure.md) · [database](architecture/database.md) |
| AmneziaWG, network or webhooks | [Pinned upstream](integrations/amneziawg.md) · [networking](architecture/networking.md) · [webhooks](integrations/webhooks.md) |
| REST API | [API contract](architecture/api.md) |
| Security | [Security model](operations/security.md) |
| Development checks or a release | [Workflow](development/workflow.md) · [test layers](development/testing.md) |

## Reference

**Product:** [requirements](product/requirements.md) · [UI/UX](product/ui-ux.md) ·
[design system](product/design-system.md) · [presentation primitives](product/presentation-system.md)

**Architecture:** [overview](architecture/overview.md) ·
[project structure](architecture/project-structure.md) · [database](architecture/database.md) ·
[API](architecture/api.md) · [networking](architecture/networking.md) ·
[egress routing](architecture/egress-routing.md) · [decisions (ADRs)](decisions/)

**Integrations:** [AmneziaWG pin](integrations/amneziawg.md) ·
[client apps and WARP](integrations/client-apps-and-warp.md) · [webhooks](integrations/webhooks.md)

**Operations:** [GitHub installation](operations/github-install.md) ·
[deployment](operations/deployment.md) · [domains and TLS](operations/domains-and-tls.md) ·
[terminal management](operations/terminal-management.md) ·
[Update Center](operations/update-center.md) · [backup and restore](operations/backup-restore.md) ·
[lifecycle recovery](operations/lifecycle-recovery.md) · [cleanup](operations/cleanup.md) ·
[runbook](operations/runbook.md) · [security](operations/security.md)

**Development:** [workflow](development/workflow.md) · [testing](development/testing.md) ·
[status](development/status.md) · [release readiness](development/release-readiness.md)

## Archive

Completed phase records, the original specification and dated status and release history live in
the [archive](archive/README.md). They are evidence of their time: consult one only when a
changed claim depends on it, and never treat an old test plan as a standing instruction.

## Keeping docs true

Update the matching living document in the same change when behavior, compatibility or a
documented claim changes; an internal change that leaves the claims true needs no doc sweep.
Keep designed, implemented, automated-tested and real-host verified apart. CI runs
`python3 scripts/check-docs.py` to check links, anchors and named repository paths.
