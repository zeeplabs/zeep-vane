# Audit Log Expansion Tasks

**Spec**: `.specs/features/audit-log-expansion/spec.md`
**Status**: In progress (T1-T5 done)

No `design.md` — no new architecture/pattern, only repeating the existing `audit.Log.Record` call already used by `StatusPagesHandler`/`DomainsHandler` across handlers that don't have it yet, plus wiring `New*Handler` constructors + `routes.go`, plus frontend i18n strings.

---

## Test Coverage Matrix

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| `ServicesHandler` (Create/Update/Delete) | integration | success path records `service_created`/`service_updated`/`service_deleted` with the right `target_label`; failure path (404/422/409) records nothing | `internal/api/services_handler_test.go` | `go test -tags=integration ./internal/api` |
| `StatusPagesHandler` (Create/AttachDomain/SetServices) | integration | same pattern for `status_page_created`/`status_page_domain_attached`/`status_page_services_updated` | `internal/api/status_pages_handler_test.go` | mesmo |
| `DomainsHandler.Create` | integration | same pattern for `domain_created` | `internal/api/domains_handler_test.go` | mesmo |
| `IntegrationsHandler.ConnectDatadog` | integration | same pattern for `datadog_connected` | `internal/api/integrations_handler_test.go` | mesmo |
| `EmailProvidersHandler` (Connect/Activate) | integration | same pattern for `email_provider_connected`/`email_provider_activated` | `internal/api/email_providers_handler_test.go` | mesmo |
| `LLMProvidersHandler` (Connect/Activate) | integration | same pattern for `llm_provider_connected`/`llm_provider_activated`; `SetModel` untouched | `internal/api/llm_providers_handler_test.go` | mesmo |
| `CompanySettingsHandler` (Update/UploadLogo) | integration | same pattern for `company_settings_updated`/`company_logo_updated` | `internal/api/company_settings_handler_test.go` | mesmo |
| `TenantHandler.Delete` | integration | same pattern for `tenant_deleted`; 409 "last active tenant" path records nothing | `internal/api/tenant_handler_test.go` | mesmo |
| Frontend i18n + audit-log card | unit/component | all 12 new actions have pt-BR + en labels; card renders a readable phrase, never the raw `action` string | `web/src/features/overview/*.test.tsx` or wherever the card's tests live | `cd web && npm run test` |

## Gate Check Commands

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Quick | Any backend task, fast check | `go build ./... && go vet ./... && gofmt -l <changed files>` |
| Full | After each backend task | disposable Postgres per AGENTS.md §3: `TEST_DATABASE_URL=... go test -tags=integration -p 1 ./internal/api/... ./internal/cli/...` |
| Frontend | T9 | `cd web && npx tsc -b --noEmit && npm run test` |
| Release gate | End of Execute | `go build ./... && go test ./... && go vet ./...`, full integration gate, frontend gate |

---

## Execution Plan

Sequential, single batch (9 tasks, no sub-agent split — each handler task briefly touches shared `routes.go`, safer done one at a time).

```
T1 → T2 → T3 → T4 → T5 → T6 → T7 → T8 → T9
```

---

## Task Breakdown

### T1: ServicesHandler audit wiring

**What**: Add `audit *audit.Log` field + constructor param to `ServicesHandler`. `Create`/`Update`/`Delete` call `Record` with `service_created`/`service_updated`/`service_deleted` (`target_label` = service name) after each succeeds, before responding. Update `routes.go` call site to pass the shared `auditLog`.
**Where**: `internal/api/services_handler.go`, `internal/api/services_handler_test.go`, `internal/cli/routes.go`
**Depends on**: None
**Reuses**: `audit.Log.Record` pattern already in `status_pages_handler.go`/`domains_handler.go`
**Requirement**: AUDITEXP-01, AUDITEXP-02, AUDITEXP-03, AUDITEXP-16, AUDITEXP-17

**Tools**: MCP NONE · Skill NONE

**Done when**:
- [ ] Create/Update/Delete each record their action with the correct `target_label` on success
- [ ] A failed Create/Update/Delete (validation error, 404, 409) records nothing
- [ ] A `Record` error is logged, never fails or reverts the response
- [ ] Gate check passes: full

**Tests**: integration
**Gate**: full

---

### T2: StatusPagesHandler audit wiring (Create/AttachDomain/SetServices)

**What**: `Create`/`AttachDomain`/`SetServices` call `Record` (handler already has `audit` field) with `status_page_created`/`status_page_domain_attached`/`status_page_services_updated` (`target_label` = status page name).
**Where**: `internal/api/status_pages_handler.go`, `internal/api/status_pages_handler_test.go`
**Depends on**: T1
**Reuses**: existing `h.audit` field, same `Record` pattern already used by this handler's `Delete`/`VerifyDomain`
**Requirement**: AUDITEXP-04, AUDITEXP-05, AUDITEXP-06, AUDITEXP-16, AUDITEXP-17

**Tools**: MCP NONE · Skill NONE

**Done when**:
- [ ] Create/AttachDomain/SetServices each record their action with the correct `target_label` on success
- [ ] Failure path records nothing
- [ ] Gate check passes: full

**Tests**: integration
**Gate**: full

---

### T3: DomainsHandler.Create audit wiring

**What**: `Create` calls `Record` (handler already has `audit` field) with `domain_created` (`target_label` = hostname).
**Where**: `internal/api/domains_handler.go`, `internal/api/domains_handler_test.go`
**Depends on**: T2
**Reuses**: existing `h.audit` field, same pattern as this handler's `Delete`/`Verify`
**Requirement**: AUDITEXP-07, AUDITEXP-16, AUDITEXP-17

**Tools**: MCP NONE · Skill NONE

**Done when**:
- [ ] Create records `domain_created` with the hostname on success
- [ ] Failure path records nothing
- [ ] Gate check passes: full

**Tests**: integration
**Gate**: full

---

### T4: IntegrationsHandler.ConnectDatadog audit wiring

**What**: Add `audit *audit.Log` field + constructor param to `IntegrationsHandler`. `ConnectDatadog` calls `Record` with `datadog_connected`. Update `routes.go`.
**Where**: `internal/api/integrations_handler.go`, `internal/api/integrations_handler_test.go`, `internal/cli/routes.go`
**Depends on**: T3 (sequential to avoid routes.go merge friction, no code dependency)
**Reuses**: `audit.Log.Record` pattern
**Requirement**: AUDITEXP-08, AUDITEXP-16, AUDITEXP-17

**Tools**: MCP NONE · Skill NONE

**Done when**:
- [ ] ConnectDatadog records `datadog_connected` on success
- [ ] Failure path (invalid credentials) records nothing
- [ ] Gate check passes: full

**Tests**: integration
**Gate**: full

---

### T5: EmailProvidersHandler audit wiring (Connect/Activate)

**What**: Add `audit *audit.Log` field + constructor param. `Connect`/`Activate` call `Record` with `email_provider_connected`/`email_provider_activated` (`target_label` = provider name). Update `routes.go`.
**Where**: `internal/api/email_providers_handler.go`, `internal/api/email_providers_handler_test.go`, `internal/cli/routes.go`
**Depends on**: T4
**Reuses**: `audit.Log.Record` pattern
**Requirement**: AUDITEXP-09, AUDITEXP-10, AUDITEXP-16, AUDITEXP-17

**Tools**: MCP NONE · Skill NONE

**Done when**:
- [ ] Connect/Activate each record their action with the correct `target_label` on success
- [ ] Failure path records nothing
- [ ] Gate check passes: full

**Tests**: integration
**Gate**: full

---

### T6: LLMProvidersHandler audit wiring (Connect/Activate)

**What**: Add `audit *audit.Log` field + constructor param. `Connect`/`Activate` call `Record` with `llm_provider_connected`/`llm_provider_activated` (`target_label` = provider name). `SetModel` untouched. Update `routes.go`.
**Where**: `internal/api/llm_providers_handler.go`, `internal/api/llm_providers_handler_test.go`, `internal/cli/routes.go`
**Depends on**: T5
**Reuses**: `audit.Log.Record` pattern
**Requirement**: AUDITEXP-11, AUDITEXP-12, AUDITEXP-16, AUDITEXP-17

**Tools**: MCP NONE · Skill NONE

**Done when**:
- [ ] Connect/Activate each record their action with the correct `target_label` on success
- [ ] Failure path records nothing
- [ ] `SetModel` still records nothing (confirms out-of-scope boundary)
- [ ] Gate check passes: full

**Tests**: integration
**Gate**: full

---

### T7: CompanySettingsHandler audit wiring (Update/UploadLogo)

**What**: Add `audit *audit.Log` field + constructor param. `Update`/`UploadLogo` call `Record` with `company_settings_updated`/`company_logo_updated`. Update `routes.go`.
**Where**: `internal/api/company_settings_handler.go`, `internal/api/company_settings_handler_test.go`, `internal/cli/routes.go`
**Depends on**: T6
**Reuses**: `audit.Log.Record` pattern
**Requirement**: AUDITEXP-13, AUDITEXP-14, AUDITEXP-16, AUDITEXP-17

**Tools**: MCP NONE · Skill NONE

**Done when**:
- [ ] Update/UploadLogo each record their action on success
- [ ] Failure path records nothing
- [ ] Gate check passes: full

**Tests**: integration
**Gate**: full

---

### T8: TenantHandler.Delete audit wiring

**What**: Add `audit *audit.Log` field + constructor param. `Delete` calls `Record` with `tenant_deleted` (`target_label` = tenant name) after `SoftDelete` succeeds, before responding 200. Update `routes.go`.
**Where**: `internal/api/tenant_handler.go`, `internal/api/tenant_handler_test.go`, `internal/cli/routes.go`
**Depends on**: T7
**Reuses**: `audit.Log.Record` pattern
**Requirement**: AUDITEXP-15, AUDITEXP-16, AUDITEXP-17

**Tools**: MCP NONE · Skill NONE

**Done when**:
- [ ] Successful delete records `tenant_deleted` with the tenant's name
- [ ] The existing 409 "last active tenant" path records nothing
- [ ] Gate check passes: full

**Tests**: integration
**Gate**: full

---

### T9: Frontend i18n for the 12 new actions

**What**: Add pt-BR + en labels for `service_created`/`service_updated`/`service_deleted`/`status_page_created`/`status_page_domain_attached`/`status_page_services_updated`/`domain_created`/`datadog_connected`/`email_provider_connected`/`email_provider_activated`/`llm_provider_connected`/`llm_provider_activated`/`company_settings_updated`/`company_logo_updated`/`tenant_deleted` in whichever action-to-label map the audit-log card already uses for the 9 existing actions.
**Where**: wherever the existing 9 actions' labels live (frontend i18n resources + the card's action-map), plus its test file
**Depends on**: T8 (needs the full final action list)
**Reuses**: existing i18n keys/pattern for the 9 current actions
**Requirement**: AUDITEXP-18

**Tools**: MCP NONE · Skill NONE

**Done when**:
- [ ] Every new action has a pt-BR and en label
- [ ] Card test covers at least one new action per entity group, asserting the readable phrase (never the raw `action` string)
- [ ] Gate check passes: frontend

**Tests**: component
**Gate**: frontend
