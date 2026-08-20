# LESSONS - auto-maintained by scripts/lessons.py

> Machine-owned. Do NOT hand-edit. Changes are overwritten on the next `lessons.py` write.
> Canonical state lives in `.specs/lessons.json`. Edit lessons only via the script.
> promote_threshold=2 distinct features · window_days=45 · quarantine_threshold=2

## Confirmed (load these at Specify/Design)

Corroborated across multiple features. Safe to apply as guidance.

_none_

## Candidates (under observation - do NOT load as guidance yet)

Seen once or not yet corroborated. Tracked, not trusted.

### L-001 - Test every route in the production route-mounting file, not one representative route per category
- signal: `surviving_mutant` · recurrence: 1 feature(s) · scope: `routes` · harmful: 0
- features: admin-dashboard
- evidence: validation.md M5/M6 - internal/cli/routes.go:69,72 (routes)
- last seen: 2026-08-20T00:25:53Z

### L-002 - Exercise authorization through the production router assembly, not a router rebuilt inside the test
- signal: `surviving_mutant` · recurrence: 1 feature(s) · scope: `routes` · harmful: 0
- features: admin-dashboard
- evidence: validation.md M8 - internal/cli/routes.go:60 (routes)
- last seen: 2026-08-20T00:25:53Z

### L-003 - When a record is deleted, assert the resulting access denial on a live request instead of only asserting the row is gone
- signal: `ac_gap` · recurrence: 1 feature(s) · scope: `auth` · harmful: 0
- features: admin-dashboard
- evidence: ADM-07 + spec.md:97 - internal/api/admins_test.go:620 (auth)
- last seen: 2026-08-20T00:25:53Z

### L-004 - Assert the exact token lifetime the spec states, reading it from the row the handler wrote
- signal: `spec_precision_gap` · recurrence: 1 feature(s) · scope: `auth` · harmful: 0
- features: admin-dashboard
- evidence: ADM-01 - internal/api/admins.go:22 (auth)
- last seen: 2026-08-20T00:25:53Z

### L-005 - Reconcile the spec text with the committed schema before implementing, instead of documenting a deviation in the handler
- signal: `spec_deviation` · recurrence: 1 feature(s) · scope: `spec` · harmful: 0
- features: admin-dashboard
- evidence: SPEC_DEVIATION internal/api/admins.go:84-93 (spec)
- last seen: 2026-08-20T00:25:53Z

## Quarantined (failed when applied - ignore)

A confirmed lesson that recurred alongside failure. Kept for the maintainer to review.

_none_
