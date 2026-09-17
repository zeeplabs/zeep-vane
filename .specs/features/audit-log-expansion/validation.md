# Audit Log Expansion — Validation Report

**Verifier**: independent (no prior context on implementation)
**Diff range**: `086d678..c8bc6f3` (9 commits, 24 files, +1271/-30)
**Verdict**: **PASS** (with 2 minor coverage gaps noted, no correctness defects found)

---

## 1. Spec-anchored outcome check (18 ACs)

| AC | Requirement | Test | Evidence | Verdict |
| --- | --- | --- | --- | --- |
| AUDITEXP-01 | `service_created`, target=name | `TestCreateService_ValidRequest_RecordsServiceCreatedAudit` | `internal/api/services_handler_test.go:1142` | PASS |
| AUDITEXP-02 | `service_updated`, target=post-edit name | `TestUpdateService_ValidRequest_RecordsServiceUpdatedAudit` | `internal/api/services_handler_test.go:1195` | PASS |
| AUDITEXP-03 | `service_deleted`, target=removed name | `TestDeleteService_Unattached_RecordsServiceDeletedAuditLabelSurvivingDelete` | `internal/api/services_handler_test.go:1222` | PASS |
| AUDITEXP-04 | `status_page_created`, target=name | `TestCreateStatusPage_ValidRequest_RecordsStatusPageCreatedAudit` | `internal/api/status_pages_handler_test.go:936` | PASS |
| AUDITEXP-05 | `status_page_domain_attached`, target=page name | `TestAttachDomain_ValidRequest_RecordsStatusPageDomainAttachedAudit` | `internal/api/status_pages_handler_test.go:963` | PASS |
| AUDITEXP-06 | `status_page_services_updated`, target=page name | `TestSetServices_ReplacesLinkedSet_RecordsStatusPageServicesUpdatedAudit` | `internal/api/status_pages_handler_test.go:988` | PASS |
| AUDITEXP-07 | `domain_created`, target=hostname | `TestCreateDomain_NewHostname_RecordsDomainCreatedAudit` | `internal/api/domains_handler_test.go:839` | PASS |
| AUDITEXP-08 | `datadog_connected` | `TestConnectDatadog_ValidCredentials_RecordsDatadogConnectedAudit` | `internal/api/integrations_handler_test.go:499` | PASS |
| AUDITEXP-09 | `email_provider_connected`, target=provider name | `TestConnectEmailProvider_ValidRequest_RecordsEmailProviderConnectedAudit` | `internal/api/email_providers_audit_integration_test.go:82` | PASS |
| AUDITEXP-10 | `email_provider_activated`, target=provider name | `TestActivateEmailProvider_ValidRequest_RecordsEmailProviderActivatedAudit` | `internal/api/email_providers_audit_integration_test.go:107` | PASS |
| AUDITEXP-11 | `llm_provider_connected`, target=provider name | `TestConnectLLMProvider_ValidRequest_RecordsLLMProviderConnectedAudit` | `internal/api/llm_providers_audit_integration_test.go:77` | PASS |
| AUDITEXP-12 | `llm_provider_activated`, target=provider name | `TestActivateLLMProvider_ValidRequest_RecordsLLMProviderActivatedAudit` | `internal/api/llm_providers_audit_integration_test.go:100` | PASS |
| AUDITEXP-13 | `company_settings_updated` | `TestCompanySettingsUpdate_ValidBody_RecordsCompanySettingsUpdatedAudit` | `internal/api/company_settings_handler_test.go:788` | PASS |
| AUDITEXP-14 | `company_logo_updated` | `TestUploadLogo_ValidPNG_RecordsCompanyLogoUpdatedAudit` | `internal/api/company_settings_handler_test.go:810` | PASS |
| AUDITEXP-15 | `tenant_deleted`, target=tenant name, before 200 | `TestDeleteTenant_SecondActiveTenantExists_RecordsTenantDeletedAuditLabelSurvivingDelete` | `internal/api/tenant_handler_test.go:170` | PASS |
| AUDITEXP-16 | Failure ⇒ no audit row | Explicit negative-path tests exist for only **3 of 13** operations: `TestCreateService_MissingName_422_NoAuditRecorded` (`services_handler_test.go:1166`), `TestConnectDatadog_InvalidCredentials_NoAuditRecorded` (`integrations_handler_test.go:521`), `TestDeleteTenant_OnlyActiveTenant_NoAuditRecorded` (`tenant_handler_test.go:206`) | **Gap** — see §4 | PARTIAL |
| AUDITEXP-17 | `Record` error ⇒ logged, request still succeeds | No test simulates a `Record` failure for any of the 15 new call sites (consistent with the pre-existing 9 actions, which are also untested for this path — `audit.Log` is a concrete struct wrapping a DB pool, not mockable without a broken connection) | **Gap** (pre-existing pattern, not a regression) | PARTIAL (code inspection confirms correct `log-and-continue` pattern at all 15 sites; not test-enforced) |
| AUDITEXP-18 | i18n phrase, never raw `action` | `web/src/features/overview/activityPhrases.test.ts:25-32` explicitly asserts 8 of the 15 new actions (one per entity group, per T9's own bar); `web/src/lib/i18n.ts:471-500` (pt-BR) and `:1048-1077` (en) define all 15 pairs; `KNOWN_ACTIVITY_ACTIONS` in `activityPhrases.ts:10-35` lists all 15 | PASS (implementation complete for all 15; test is a representative sample, not exhaustive) |

## 2. Discrimination sensor (mutation testing)

Performed in an isolated git worktree (`/tmp/audit-log-verify-scratch`, branch `audit-log-verify-scratch`, checked out at `c8bc6f3`) against a disposable Postgres container (port 5436, destroyed after). 4 mutants across 4 different files, all **killed**:

| # | File:line | Mutation | Test that failed |
| --- | --- | --- | --- |
| 1 | `services_handler.go:213` (Create) | action string `"service_created"` → `"service_created_MUTANT"` | `TestCreateService_ValidRequest_RecordsServiceCreatedAudit` — asserted `("service_created_MUTANT", ...)` vs expected `("service_created", ...)` |
| 2 | `tenant_handler.go:89` (Delete) | entire `audit.Record` call commented out | `TestDeleteTenant_SecondActiveTenantExists_RecordsTenantDeletedAuditLabelSurvivingDelete` — `no rows in result set` |
| 3 | `email_providers_handler.go:117` (Connect) | `target_label` → `""` | `TestConnectEmailProvider_ValidRequest_RecordsEmailProviderConnectedAudit` — `cannot scan NULL into *string` (empty string persists as NULL, test expects `"SendGrid"`) |
| 4 | `status_pages_handler.go:214` (AttachDomain) | action string → `"status_page_domain_attached_MUTANT"` | `TestAttachDomain_ValidRequest_RecordsStatusPageDomainAttachedAudit` — `no rows in result set` |

No surviving mutants. Worktree reverted to clean before removal; `git status --porcelain` on the real tree showed only pre-existing unrelated files (`web/src/features/profile/*`, `web/src/features/two-factor/*`, `.specs/features/provider-disconnect/`, `node_modules/`) — confirmed untouched by this verification.

## 3. Full gate re-run (real tree, `develop` @ `c8bc6f3`)

| Gate | Result |
| --- | --- |
| `go build ./...` | clean |
| `go vet ./...` | clean |
| `gofmt -l` on all `.go` files touched by the 9 commits | clean (no output) |
| `go test ./...` (no tag) | all packages `ok` |
| `go test -tags=integration -p 1 ./...` (disposable Postgres, port 5433, destroyed after) | all packages `ok`, including the three known-flaky tests (`TestPostgresStorage_Lock_OutOfBandKill_AutoReleases`, `TestEmailProviderRepository_SetActiveProvider_UpdatesSingletonRow`, `TestLLMProviderRepository_SetActiveProvider_ThenGetActiveProvider_RoundTrips`) — none flaked this run |
| `cd web && npx tsc -b --noEmit` | clean |
| `cd web && npm run test` | 96 test files / 658 tests passed |

No regressions. `vane-dev-pg` was never touched; both disposable containers (`vane-test-pg` :5433, `vane-verify-pg` :5436) were stopped/auto-removed after use.

## 4. Findings (ranked)

1. **[Minor] AUDITEXP-16 coverage gap** — the spec requires that a failed operation never write an audit row, and every task's "Done when" checklist (T1–T8) explicitly lists "failure path records nothing" as a completion criterion, but only 3 of 13 audited operations (`ServicesHandler.Create`, `IntegrationsHandler.ConnectDatadog`, `TenantHandler.Delete`) have an explicit test proving it. `StatusPagesHandler` (Create/AttachDomain/SetServices), `DomainsHandler.Create`, `EmailProvidersHandler` (Connect/Activate), `LLMProvidersHandler` (Connect/Activate), `CompanySettingsHandler` (Update/UploadLogo), and `ServicesHandler.Update/Delete` have no negative-path assertion. Code inspection shows all `Record` calls are correctly placed after the business operation succeeds (no call site is reachable from an error branch), so this is a test-coverage gap, not an observed defect — but it means a future regression on those 10 operations (e.g., someone hoists a `Record` call above its success check) would not be caught by the suite.
2. **[Minor] AUDITEXP-17 coverage gap** — no test anywhere (including the 9 pre-existing actions) exercises the `Record` error path (logged, request still succeeds). This is a pre-existing limitation of `audit.Log` being a concrete DB-backed struct rather than an injectable interface — not something this feature regressed, and not blocking, but worth flagging since the spec calls it out as AC-17.
3. **[Informational] AUDITEXP-18 test is a sample, not exhaustive** — `activityPhrases.test.ts` explicitly asserts 8 of the 15 new actions (one per entity group). The other 7 (`service_updated`, `service_deleted`, `status_page_domain_attached`, `status_page_services_updated`, `email_provider_activated`, `llm_provider_connected`, `company_logo_updated`) are covered by the implementation (`i18n.ts`, `activityPhrases.ts`) but not by an explicit test assertion. This matches T9's own stated bar ("at least one new action per entity group") — not a violation of the task, just noted for completeness.

No surviving mutants, no build/vet/format/test regressions, no spec deviations found in the 15 success-path call sites inspected.
