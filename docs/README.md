# WG-Guard documentation

These are the living contracts for the Go/SQLite/HTMX AmneziaWG node panel. Choose the smallest
set of documents relevant to the current task; this map is a route, not a reading checklist.
Implementation and test evidence must support any claimed behavior.

## Choose a route

| Need | Start with |
|---|---|
| Current capability, supported host or verification claim | [Status matrix](development/status.md); use [release readiness](development/release-readiness.md) for blockers, certification history and release decisions |
| Product behavior, panel UX or public API | [Requirements](product/requirements.md), [UI/UX contract](product/ui-ux.md), [API contract](architecture/api.md) as relevant |
| Architecture or data change | [Overview](architecture/overview.md), [project structure](architecture/project-structure.md), [database](architecture/database.md) |
| AmneziaWG, network or webhook change | [Pinned upstream](integrations/amneziawg.md), [networking](architecture/networking.md), [webhook contract](integrations/webhooks.md) |
| Security, deployment or recovery | [Security model](operations/security.md), [deployment](operations/deployment.md), [runbook](operations/runbook.md); follow the specific operations guide below |
| Development checks or release work | [Workflow](development/workflow.md); consult [test layers](development/testing.md) for specialized coverage and [Phase 12](development/phase12.md) for the published v0.1.0 evidence |

## Reference index

**Product and design:** [requirements](product/requirements.md) ·
[UI/UX](product/ui-ux.md).

**Architecture and integrations:** [overview](architecture/overview.md) ·
[project structure](architecture/project-structure.md) · [database](architecture/database.md) ·
[API](architecture/api.md) · [networking](architecture/networking.md) ·
[AmneziaWG pin and verification](integrations/amneziawg.md) ·
[webhooks](integrations/webhooks.md) · [ADRs](decisions/).

**Operations:** [deployment](operations/deployment.md) ·
[GitHub installation and acquisition](operations/github-install.md) ·
[English terminal management](operations/terminal-management.md) ·
[lifecycle recovery](operations/lifecycle-recovery.md) ·
[Update Center](operations/update-center.md) · [account/address cleanup](operations/cleanup.md) ·
[backup and restore](operations/backup-restore.md) · [runbook](operations/runbook.md) ·
[security](operations/security.md).

**Development and current claims:** [workflow](development/workflow.md) ·
[testing](development/testing.md) · [status](development/status.md) ·
[release readiness](development/release-readiness.md) · [roadmap](../ROADMAP.md).

**Completed phase evidence:** [Phase 8](development/phase8.md) ·
[8.1](development/phase8.1.md) · [8.2](development/phase8.2.md) ·
[8.3](development/phase8.3.md) · [9](development/phase9.md) ·
[10](development/phase10.md) · [11](development/phase11.md) ·
[12](development/phase12.md). Read only the record relevant to a changed claim or acceptance
boundary; its historical test plan is not a standing instruction for routine edits.

**Frozen provenance:** [original specification](archive/wg-guard_SPEC.md),
[initial upstream research](archive/INITIAL_DELIVERABLE.md), and
[architecture proposal](archive/ARCHITECTURE_V2_PROPOSAL.md). These preserve origin and context;
the living contracts and current status above govern new work. Consult the initial research for
upstream facts absent from the pinned integration document, then verify the relevant version
before making a new claim. Test fixtures retain their recorded form for reproducibility.

Update the matching living document in the same change when behavior, compatibility or a
documented claim changes. An internal change that leaves those claims true does not require
touching unrelated documents. Distinguish designed, implemented, automated-test verified and
real-host verified behavior; do not infer one level from another.
