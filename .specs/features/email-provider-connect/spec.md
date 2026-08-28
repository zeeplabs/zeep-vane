# Email Provider Connect Specification

## Problem Statement

Vane has no way to send transactional email. The admin-invite resend/cancel feature (backlog item, `AD-007`) needs an email to actually reach the invited person, and every future email Vane sends (invite, and later others) needs a provider to send through. Self-hosted installs each run their own infra, so the provider and its credentials belong to the instance owner, not to Vane itself.

## Goals

- [ ] Instance owner can connect SendGrid and/or Resend with their own API key, validated live before being stored.
- [ ] Exactly one connected provider is "active" at any time; switching is instant, no reconnect required.
- [ ] A provider-agnostic admin-invite email template exists and can be sent through whichever provider is active, via a Go interface the future resend/cancel-invite feature calls.

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
| --- | --- |
| Wiring the admin-invite resend/cancel HTTP endpoints themselves | Separate, already-identified backlog feature (`AD-007`); this feature only builds what it depends on (provider connection + send capability + template). |
| Disconnecting/removing a connected provider | Not requested. Reconnecting (same as Datadog's upsert) already lets the owner overwrite a bad key; deletion can be added later without reshaping this design. |
| Editable/DB-stored templates, template management UI | User chose "only the admin-invite template" scope; a template-registry abstraction for future email types is over-engineering for one template. Template is a Go-embedded `html/template` + plain-text fallback, versioned in code like everything else in this repo. |
| Any provider besides SendGrid and Resend | User named exactly these two for the MVP; the `EmailProvider` interface is the extension point for more later. |
| Retry/backoff/queue on send failure | User explicitly chose synchronous, no-retry send for this round. |
| "Send test email" button | Not requested; connect-time validation (below) already confirms the key works before it's stored. |
| Reply-To header, CC/BCC, attachments | Not requested; admin-invite email needs only To/Subject/Body. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| SendGrid credential-validation call | `GET /v3/scopes` with the submitted API key; 200 = valid, 401 = invalid/missing key. No email is sent to validate. | [Certo] Confirmed via Twilio/SendGrid docs (`docs.sendgrid.com/api-reference/api-key-permissions/retrieve-a-list-of-scopes-for-which-this-user-has-access`) during Design research: returns the calling key's scopes with no side effects; 401 = invalid/malformed/revoked key, 403 = valid key lacking permission for a given action (not relevant here, this call needs no specific scope). Mirrors how `datadog.ValidateCredentials` uses a minimal read call instead of a mutating one. | y (research-confirmed) |
| Resend credential-validation call | `GET https://api.resend.com/api-keys` (List API Keys) with the submitted key; 200 = valid, 401 = invalid/missing key. | [Certo]/[Provável] endpoint and 200 response shape confirmed live via `resend.com/docs/api-reference/api-keys/list-api-keys` during Design research (`{object, has_more, data: [{id, name, created_at, last_used_at}]}`). The 401-on-invalid-key behavior is [Provável] - not explicitly documented on that page, inferred from standard REST convention and Resend's own error-handling docs, not live-tested against a real invalid key. Switched from the originally assumed `GET /domains`: listing API keys has no side effects and does not depend on the owner having verified a sending domain yet (a fresh Resend account may have zero domains and would otherwise look invalid even with a working key). | y (research-confirmed) |
| `from_email` / `from_name` ownership | Stored per provider connection (`email_providers.from_email`, `.from_name`), not reused from `company_settings.contact_email`. | User initially proposed a fixed `@zeeptecnologia` sender; flagged as a deliverability risk - SendGrid/Resend require the `from` domain to be verified **in the specific provider account whose API key is authenticating the send**, and each self-hosted install owns its own provider account (`AD-002`: one company per install). A shared/foreign sender domain would be rejected as spoofing on every install except Zeep's own. User confirmed the per-provider field after this was raised. | y |
| Admin invite email's sender display name | `from_name` submitted at connect time (free text), independent of `company_settings.name`. | Owner explicitly sets it per provider at connect time; no requirement was stated to derive it from company settings, and hardcoding that coupling isn't asked for. | y (by default - not raised as a separate question, but consistent with "from_email per provider" decision) |
| Encryption at rest for the API key | Reuse `internal/crypto.Encrypt`/`Decrypt` with `cfg.MasterKey`, exactly like `IntegrationRepository`'s Datadog keys. | Existing, already-tested pattern in this exact codebase for "encrypt an external provider's API key at rest" (`internal/db/integration_repository.go`, `internal/api/integrations_handler.go`). No reason to diverge. | y (derived from Knowledge Verification Chain Step 1 - codebase pattern, not asked as a question) |
| Auth boundary for connect/activate/list | `writeRoles` (owner, operator) for connect and activate; `anyRole` (owner, operator, viewer) for list/status - identical split to the existing Datadog integration routes. | Mirrors `internal/cli/routes.go`'s existing role split for `/api/integrations/datadog/*`; no reason for email credentials to be less protected than Datadog's. | y (derived from codebase, not asked) |
| Behavior when `Send` is called with no active provider | Return a typed `ErrNoActiveProvider`; caller (the future invite endpoint) decides how to surface it. | Consistent with the chosen "synchronous, no retry" failure model - this feature doesn't own what the caller does with the error, only that the error is unambiguous and typed. | y (derived from "erro síncrono, sem retry" decision) |

**Open questions:** none - all resolved or logged above.

---

## User Stories

### P1: Connect an email provider ⭐ MVP

**User Story**: As the instance owner, I want to connect SendGrid or Resend with my own API key and sender address, so that Vane can send transactional email through my account.

**Why P1**: Nothing else in this feature works without a stored, validated provider connection.

**Acceptance Criteria**:

1. WHEN the owner submits `POST /api/integrations/email/{provider}` (`provider` = `sendgrid` or `resend`) with `api_key`, `from_email`, `from_name` THEN the system SHALL call that provider's validation endpoint with the submitted key before persisting anything.
2. IF the provider validation call returns 401/403, or `api_key`/`from_email`/`from_name` is missing, or `from_email` is not a syntactically valid email address THEN the system SHALL respond `422` and SHALL NOT write or encrypt anything.
3. WHEN validation succeeds THEN the system SHALL encrypt `api_key` with `internal/crypto.Encrypt` and upsert one row per `provider` (`email_providers`, unique on `provider`) with `status = 'connected'`, `from_email`, `from_name`, clearing any previous `last_error`.
4. IF `provider` is not `sendgrid` or `resend` THEN the system SHALL respond `404`.
5. The system SHALL NOT echo `api_key` back in any response body or log line, in plaintext or ciphertext.
6. WHEN a provider is reconnected (same `provider` value submitted again) THEN the system SHALL overwrite the existing row's key/from fields (upsert), not create a second row.

**Independent Test**: `POST /api/integrations/email/sendgrid` with a stubbed validator that accepts, then confirm the row exists via `GET /api/integrations/email` with `status: "connected"` and no key material in the response.

---

### P1: Switch the active provider ⭐ MVP

**User Story**: As the instance owner, I want to pick which connected provider is used to send email, so that I can switch providers without reconnecting.

**Why P1**: User explicitly required "only one usable at a time" with instant switching between already-connected providers - this is the mechanism.

**Acceptance Criteria**:

1. WHEN the owner submits `POST /api/integrations/email/{provider}/activate` for a `provider` with `status = 'connected'` THEN the system SHALL set `email_settings.active_provider = provider` (singleton row, same pattern as `company_settings`).
2. IF `provider` has no connected row (never connected, or `status = 'invalid'`) THEN the system SHALL respond `422` and SHALL NOT change `active_provider`.
3. The system SHALL allow at most one `active_provider` value at any time (single nullable column on a singleton row - structurally cannot hold two).
4. WHEN no provider has ever been activated THEN `active_provider` SHALL be `NULL` and `GET /api/integrations/email` SHALL report no provider as active.

**Independent Test**: Connect both `sendgrid` and `resend` (stubbed validators), activate `resend`, confirm `GET /api/integrations/email` reports `resend` active and `sendgrid` connected-but-inactive; activate `sendgrid`, confirm the flip.

---

### P1: List connected providers and their status ⭐ MVP

**User Story**: As the instance owner, I want to see which providers are connected, their status, and which one is active, so I can verify the connection before relying on it.

**Why P1**: Both connect and activate need a way to be observed/tested independently, and the future admin UI (P2) needs this endpoint to exist first.

**Acceptance Criteria**:

1. WHEN `GET /api/integrations/email` is called THEN the system SHALL return every row in `email_providers` (`provider`, `status`, `from_email`, `from_name`, `last_checked_at`, `last_error`) plus the current `active_provider`, and SHALL NOT include `api_key` in any form.
2. WHILE no provider has ever been connected THEN the system SHALL return an empty provider list and `active_provider: null` (not a `404`).

**Independent Test**: Call the endpoint with zero, one, and two connected providers; assert the response shape and absence of key material in each case.

---

### P1: Send the admin-invite email through the active provider ⭐ MVP

**User Story**: As a future caller (the admin-invite resend/cancel feature), I want a single Go interface to render and send the admin-invite email regardless of which provider is active, so that I don't need provider-specific code at the call site.

**Why P1**: This is the actual dependency the resend/cancel-invite feature is blocked on - everything else in this feature exists to make this call possible.

**Acceptance Criteria**:

1. The system SHALL expose an `EmailSender` interface with a method to send the admin-invite email, taking at minimum the recipient address, the company/inviter display context, and the invite acceptance link.
2. WHEN `EmailSender.SendAdminInvite` is called AND a provider is active AND connected THEN the system SHALL render the admin-invite template (HTML body + plain-text fallback, both provider-agnostic) and SHALL call that provider's send API with the decrypted `api_key` and the stored `from_email`/`from_name`.
3. IF no provider is active (`active_provider IS NULL`) THEN `EmailSender.SendAdminInvite` SHALL return a typed `ErrNoActiveProvider` and SHALL NOT attempt any network call.
4. IF the active provider's send API call fails (timeout, 5xx, 401 from a since-revoked key) THEN `EmailSender.SendAdminInvite` SHALL return the error to its caller unmodified by any retry - no retry, no queue (per this round's chosen failure model).
5. The rendered admin-invite template SHALL include the invite acceptance link, the invited email's role, and the inviting company's display name.

**Independent Test**: Unit-test `EmailSender.SendAdminInvite` against a stub `EmailProvider` double for: active+connected (send called with rendered content), no active provider (typed error, zero calls), provider send failure (error propagated, zero retries).

---

### P2: Admin UI to connect, list, and switch providers

**User Story**: As the instance owner, I want an admin screen to connect SendGrid/Resend, see their status, and switch the active one, so I don't have to call the API by hand.

**Why P2**: Requested for this round ("Backend + tela admin"), but the backend stories above are independently completable and testable first - this story is the UI on top of an already-working API, same layering as the existing Datadog Integrations page.

**Acceptance Criteria**:

1. WHEN the owner opens the email-providers admin screen THEN the system SHALL display each provider (SendGrid, Resend) with its connection state (not connected / connected / invalid), and which one is active.
2. WHEN the owner submits the connect form for a provider (API key, from email, from name) THEN the system SHALL call `POST /api/integrations/email/{provider}` and SHALL show the `422` validation error inline on failure, matching the existing Datadog connect-form pattern (`SettingsPage`/Integrations page conventions already in `web/src/features/`).
3. WHEN the owner clicks "activate" on a connected, non-active provider THEN the system SHALL call the activate endpoint and SHALL reflect the new active provider without a full page reload (client cache update, same pattern as existing `hooks.ts` mutations in this codebase).
4. IF a provider's `status` is `invalid` (a manual DB edit or a future automated check, not produced by any flow in this spec) THEN the UI SHALL surface `last_error` next to that provider.

**Independent Test**: Render the page against MSW-mocked `GET/POST /api/integrations/email*` (same test infra as `StatusPageDetail.test.tsx`), assert connect/activate flows update the displayed state.

---

## Edge Cases

- IF `provider` path segment is anything other than `sendgrid`/`resend` (connect or activate) THEN system SHALL respond `404`.
- IF the connect request body is malformed JSON THEN system SHALL respond `422` (matches `ConnectDatadog`'s existing decode-failure handling).
- IF `from_email` is present but not a valid email address (basic RFC 5322 shape check, no MX/deliverability check) THEN system SHALL respond `422`.
- WHEN a previously `connected` provider is reconnected with a now-invalid key THEN system SHALL respond `422` and SHALL leave the previously stored (still valid) row untouched - a failed reconnect never overwrites a working connection (mirrors `ConnectDatadog`'s "validate before persist" guarantee).
- IF the active provider is later found to have a bad key only at send time (not at connect time) THEN `SendAdminInvite` SHALL surface the provider's error; this feature does NOT flip `status` to `invalid` automatically on a send failure (no automated health-check loop exists for email, unlike the Datadog poller) - `status` only changes via a new connect attempt.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| EMAIL-01 | P1: Connect an email provider | Design | Implementing |
| EMAIL-02 | P1: Connect an email provider | Design | Pending |
| EMAIL-03 | P1: Connect an email provider | Design | Implementing |
| EMAIL-04 | P1: Switch the active provider | Design | Implementing |
| EMAIL-05 | P1: Switch the active provider | Design | Pending |
| EMAIL-06 | P1: List connected providers and their status | Design | Implementing |
| EMAIL-07 | P1: Send the admin-invite email through the active provider | Design | Pending |
| EMAIL-08 | P1: Send the admin-invite email through the active provider | Design | Pending |
| EMAIL-09 | P1: Send the admin-invite email through the active provider | Design | Pending |
| EMAIL-10 | P2: Admin UI to connect, list, and switch providers | - | Pending |

**ID format:** `EMAIL-[NUMBER]`

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 10 total, 0 mapped to tasks, 10 unmapped ⚠️ (Tasks phase not yet run)

---

## Success Criteria

- [ ] Owner can connect SendGrid with a real key, see it listed as `connected`, activate it, and `EmailSender.SendAdminInvite` successfully calls SendGrid's send API through a real (or sandbox) key.
- [ ] Owner can do the same for Resend.
- [ ] Switching active provider between two connected providers requires zero reconnects and takes effect on the next `SendAdminInvite` call.
- [ ] An invalid key is rejected at connect time (`422`) and never reaches storage, matching the Datadog integration's existing guarantee.
- [ ] Admin UI reflects connect/activate state changes without a page reload.
