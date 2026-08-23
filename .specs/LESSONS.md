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

### L-006 - Never size a boundary test's payload from the constant under test; assert the spec's literal limit so widening it fails a test.
- signal: `surviving_mutant` · recurrence: 1 feature(s) · scope: `go/handlers/validation` · harmful: 0
- features: company-settings
- evidence: internal/api/company_settings_handler_test.go:314 (mutant M1: maxLogoBytes 10<<20 -> 100<<20 survived) (go/handlers/validation)
- last seen: 2026-08-23T15:02:30Z

### L-007 - When a task's Done-when list is narrower than its layer's Test Coverage Matrix row, treat the matrix as binding and add the missing status-code case.
- signal: `ac_gap` · recurrence: 1 feature(s) · scope: `tasks/coverage-matrix` · harmful: 0
- features: company-settings
- evidence: SET-13 (no test for uploads.Save failure -> 500) (tasks/coverage-matrix)
- last seen: 2026-08-23T15:02:31Z

### L-008 - Verify a stdlib sniffing/parsing helper actually supports every format the spec allowlists before designing validation around it.
- signal: `spec_deviation` · recurrence: 1 feature(s) · scope: `go/design/mime` · harmful: 0
- features: company-settings
- evidence: internal/api/company_settings_handler.go:189 SPEC_DEVIATION (http.DetectContentType has no SVG signature) (go/design/mime)
- last seen: 2026-08-23T15:02:31Z

### L-009 - MSW handlers under jsdom cannot read a multipart body, so verify upload field-name/payload contracts by inspecting the FormData in-process instead of in the handler.
- signal: `spec_precision_gap` · recurrence: 1 feature(s) · scope: `web/msw/uploads` · harmful: 0
- features: company-settings
- evidence: web/src/test/msw/handlers.ts:566 (multipart body unreadable under jsdom; formData() and text() both hang) (web/msw/uploads)
- last seen: 2026-08-23T15:02:31Z

## Quarantined (failed when applied - ignore)

A confirmed lesson that recurred alongside failure. Kept for the maintainer to review.

_none_
