# Provider Disconnect Specification

## Problem Statement

Email providers (SendGrid, Resend) and LLM providers (OpenAI) can be connected and activated, but there is no way to remove one afterward. A connected provider's encrypted API key stays in `email_providers`/`llm_providers` permanently, even after the admin stops using it, wants to rotate to a different account, or connected the wrong provider by mistake. This is both an operational gap (no way to clean up) and a lingering-credential concern (a revoked/rotated key an admin believes is "gone" is still stored, encrypted, in the database).

## Goals

- [ ] An owner/operator can remove a connected email or LLM provider's stored credentials from the admin UI.
- [ ] Disconnecting the currently active provider is allowed and leaves the system with no active provider (not blocked, not silently left dangling).
- [ ] Every disconnect is audited, mirroring the existing `_connected`/`_activated` audit trail.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Automatic reconnection prompt / "you have no active provider" banner | Separate UX concern; this feature only makes disconnect possible and safe at the data layer. |
| Bulk disconnect (all providers at once) | No user story motivates it; one provider at a time mirrors Connect/Activate. |
| Soft-delete / undo / restore of a disconnected provider | The stored value is a rotatable API key, not domain history (unlike Service soft-delete) - the admin re-enters the key to reconnect. Hard delete is correct here. |
| Retaining `last_error`/`last_checked_at` history after disconnect | Row is fully removed; nothing to retain. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Scope: email only vs. email + LLM | Email + LLM, one spec, mirrored requirements | Identical schema (`email_providers`/`email_settings` and `llm_providers`/`llm_settings`, both with `active_provider ... ON DELETE SET NULL`), identical service/handler shape. One spec avoids reopening this for LLM later. | y |
| Disconnecting the active provider | Allowed - the provider row is deleted and `active_provider` is cleared to NULL by the existing FK (`ON DELETE SET NULL`), no manual clear step needed in the service | User's explicit choice. The FK was already built anticipating exactly this ("keeps a future disconnect feature from leaving a dangling reference" - `0016_email_providers.up.sql`). | y |
| Confirmation before disconnect | Frontend shows a confirmation dialog before calling the endpoint | Destructive, no undo (API key is gone; admin must re-enter it to reconnect). Mirrors the existing delete-tenant confirmation dialog (`SettingsPage.tsx`'s `deleteDialogOpen`). | y |
| HTTP method / route shape | `DELETE /api/integrations/email/{provider}` and `DELETE /api/integrations/llm/{provider}`, 204 on success | Matches the existing DELETE convention (`DELETE /api/status-pages/{id}`, `DELETE /api/domains/{id}`, `ServicesHandler.Delete` → 204 No Content) rather than inventing a `/disconnect` POST sub-route. | n - reasonable default, flag if a POST sub-route is preferred for symmetry with `/activate` |
| Authorization | Same `writeRoles` (owner/operator) as Connect/Activate; viewer forbidden | Disconnect is a write/destructive action, same class as Connect/Activate, which already gate on `writeRoles` in `routes.go`. | y (inferred from existing Connect/Activate routes, not re-asked) |
| Unknown provider name (e.g. `/api/integrations/email/mailgun`) | 404, same `writeUnknownEmailProvider`/`writeUnknownLLMProvider` body Connect/Activate already use | Consistent with existing unknown-provider handling; no new error shape needed. | y (inferred from existing pattern) |
| Provider never connected (row doesn't exist) | 404 with a fixed generic body (distinct from "unknown provider name") | Mirrors `ServicesHandler.Delete`'s `db.ErrNotFound` → 404 convention; disconnecting something that was never connected is a not-found condition, not a validation error. | y (inferred from existing pattern) |
| Audit action names | `email_provider_disconnected`, `llm_provider_disconnected` | Direct mirror of `email_provider_connected`/`_activated` and `llm_provider_connected`/`_activated`. | y (inferred from existing naming convention) |
| Concurrent disconnect (double-click, two tabs) | Second call also returns 204 (idempotent delete - `DELETE FROM ... WHERE provider = $1` affecting 0 rows is still success) rather than 404 | Standard idempotent-DELETE semantics; avoids a race where two legitimate clicks produce one error toast for no reason. | n - reasonable default, flag if strict 404-on-already-gone is preferred |

**Open questions:** none - all resolved or logged above.

---

## User Stories

### P1: Disconnect an email provider ⭐ MVP

**User Story**: As an owner/operator, I want to remove a connected email provider's stored credentials so that I can stop using a provider I no longer want configured (wrong account, rotating keys, decommissioning).

**Why P1**: This is the core gap reported - no way to remove a configured email provider today.

**Acceptance Criteria**:

1. WHEN an owner/operator sends `DELETE /api/integrations/email/{provider}` for a connected provider THEN the system SHALL delete that provider's row from `email_providers` and respond `204 No Content`.
2. WHEN the disconnected provider was the active provider THEN the system SHALL clear `email_settings.active_provider` to NULL as part of the same delete (via the existing FK `ON DELETE SET NULL` - no separate service-layer step).
3. WHEN the disconnected provider was not the active provider THEN the system SHALL leave `email_settings.active_provider` unchanged.
4. IF `{provider}` is not a recognized email provider name THEN the system SHALL respond `404` with the existing unknown-provider body, unchanged from Connect/Activate.
5. IF `{provider}` is recognized but has no row in `email_providers` (never connected, or already disconnected) THEN the system SHALL respond `204 No Content` without error (idempotent delete).
6. WHEN the disconnect succeeds THEN the system SHALL record an audit entry with action `email_provider_disconnected`, the acting admin, and the provider's display name - same shape as the `_connected`/`_activated` entries.
7. IF the requesting admin's role is `viewer` THEN the system SHALL respond `403`, same as Connect/Activate.

**Independent Test**: Connect SendGrid, activate it, call `DELETE /api/integrations/email/sendgrid`, confirm 204, confirm `GET /api/integrations/email` shows no `sendgrid` row and `active_provider: null`.

---

### P1: Disconnect an LLM provider ⭐ MVP

**User Story**: As an owner/operator, I want to remove a connected LLM provider's stored credentials so that I can stop using an LLM provider I no longer want configured.

**Why P1**: Same gap as email, same schema, same urgency - shipping one without the other leaves an inconsistent admin experience.

**Acceptance Criteria**:

1. WHEN an owner/operator sends `DELETE /api/integrations/llm/{provider}` for a connected provider THEN the system SHALL delete that provider's row from `llm_providers` and respond `204 No Content`.
2. WHEN the disconnected provider was the active provider THEN the system SHALL clear `llm_settings.active_provider` to NULL via the existing FK, same mechanism as email.
3. IF `{provider}` is not a recognized LLM provider name THEN the system SHALL respond `404` with the existing unknown-provider body.
4. IF `{provider}` is recognized but has no row in `llm_providers` THEN the system SHALL respond `204 No Content` without error (idempotent delete).
5. WHEN the disconnect succeeds THEN the system SHALL record an audit entry with action `llm_provider_disconnected`.
6. IF the requesting admin's role is `viewer` THEN the system SHALL respond `403`.

**Independent Test**: Connect OpenAI, call `DELETE /api/integrations/llm/openai`, confirm 204, confirm `GET /api/integrations/llm` shows no `openai` row.

---

### P1: Disconnect button with confirmation in the admin UI ⭐ MVP

**User Story**: As an owner/operator, I want a "Disconnect" button next to each connected provider (email and LLM settings screens) so that I don't need to call the API directly.

**Why P1**: The backend endpoint alone doesn't solve the user's actual problem - there's no UI trigger today.

**Acceptance Criteria**:

1. WHEN an owner/operator clicks "Disconnect" next to a connected provider THEN the system SHALL show a confirmation dialog naming the provider before calling the DELETE endpoint.
2. WHEN the admin confirms THEN the system SHALL call the DELETE endpoint and, on success, refresh the provider list (same `invalidateQueries` pattern `useActivateEmailProvider` already uses).
3. WHEN the admin cancels the dialog THEN the system SHALL make no API call and leave the provider connected.
4. IF the DELETE call fails (network/5xx) THEN the system SHALL show an error toast/message and leave the provider list unchanged, same failure-handling posture as the existing Activate button.
5. The system SHALL disable the "Disconnect" button while the mutation is pending (same `disabled={activateMutation.isPending}` pattern the Activate button uses), to prevent double-submit.

**Independent Test**: Click Disconnect on a connected SendGrid row, cancel - row still shows connected; click Disconnect again, confirm - row disappears from the list.

---

## Edge Cases

- IF the provider being disconnected is currently active AND the only connected provider THEN the system SHALL still disconnect it, leaving zero connected providers and `active_provider: null` (accepted per the confirmed decision - no block).
- IF disconnect is called twice in quick succession (double-click, two tabs) THEN the system SHALL treat the second call as a no-op success (`204`), not an error (idempotent delete, per Assumptions).
- WHEN no provider is active after a disconnect THEN email sends SHALL continue to fail with the existing `ErrNoActiveProvider` path (EMAIL-08) - already-built behavior, not new to this feature, listed here only to confirm this feature does not need to change it.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| PROVDISC-01 | P1: Disconnect an email provider (AC1-3) | Design | Pending |
| PROVDISC-02 | P1: Disconnect an email provider (AC4-5, unknown/idempotent) | Design | Pending |
| PROVDISC-03 | P1: Disconnect an email provider (AC6-7, audit/authz) | Design | Pending |
| PROVDISC-04 | P1: Disconnect an LLM provider (AC1-2) | Design | Pending |
| PROVDISC-05 | P1: Disconnect an LLM provider (AC3-4, unknown/idempotent) | Design | Pending |
| PROVDISC-06 | P1: Disconnect an LLM provider (AC5-6, audit/authz) | Design | Pending |
| PROVDISC-07 | P1: Disconnect button with confirmation (AC1-3) | Design | Pending |
| PROVDISC-08 | P1: Disconnect button with confirmation (AC4-5) | Design | Pending |

**ID format:** `PROVDISC-[NUMBER]`

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 8 total, 0 mapped to tasks, 8 unmapped ⚠️ (tasks not yet created)

---

## Success Criteria

- [ ] An owner/operator can disconnect a connected email or LLM provider from the settings UI, with a confirmation step, and the row is gone from both the UI and `email_providers`/`llm_providers`.
- [ ] Disconnecting the active provider never errors and always leaves `active_provider` NULL.
- [ ] Every disconnect produces exactly one audit entry (`email_provider_disconnected` / `llm_provider_disconnected`).
- [ ] `go test ./...` and `npm run test` stay green with new tests covering: happy path, active-provider clear, unknown provider (404), never-connected/idempotent (204), viewer forbidden (403).
