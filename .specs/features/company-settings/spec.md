# Company Settings Specification

## Problem Statement

`SettingsPage` (company name, contact e-mail, logo) has shipped as 100% mocked UI since AD-006, with no backend behind it (`mockData.companySettings`), and was explicitly deferred as backlog in AD-007. The public status page preview (I12) also borrows the same mock for `company_name`/`logo_url`, so today's public preview never reflects what an owner actually configures. This feature builds the real backend and removes both mocks.

## Goals

- [ ] Owner can view and edit company name + contact e-mail; changes persist across restarts and are returned by `GET /api/company-settings`.
- [ ] Owner can upload/replace a company logo (PNG/SVG); the file survives a pod restart in Docker/Kubernetes deployments (persistent volume, not container-local disk or DB blob).
- [ ] Public status pages (production Host-routed and dev/preview by ID) render the real company name/logo instead of `mockData.companySettings`.

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature                                                          | Reason                                                                                             |
| ----------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| Multi-tenant company settings (per-org rows)                    | Vane has no multi-tenant concept anywhere else in the codebase; settings are a single-instance singleton. |
| Cloud object storage (S3-compatible) / CDN for the logo          | Self-hosted target with a mounted volume is the only requirement given; adding a storage abstraction is unrequested infra scope. |
| Multi-replica shared-storage guarantee beyond "mount a volume"   | Making the logo consistent across N replicas without a shared (RWX) volume is a deployment concern, not app code; documented as an assumption, not built. |
| Image resizing/optimization, logo version history                | Not requested; adds processing/storage complexity with no stated user need. |
| Admin invite resend/cancel                                       | Separate AD-007 backlog item, tracked independently. |
| 404 test coverage gap on incident update                        | Separate AD-007 backlog item, tracked independently. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --------------------- | --------------- | --------- | ---------- |
| Logo storage backend | Local filesystem path read from a new `UPLOADS_DIR` env var (default `./data/uploads` for dev), following the existing `config.Config` pattern | User confirmed: self-hosted, Docker/Kubernetes, must survive pod restart -> needs a path operators mount a persistent volume onto; no object storage requested | y |
| Multi-replica consistency | Not solved in code; documented as an operational requirement (mount a shared/RWX volume, or run single-replica) | Building a storage abstraction (S3, etc.) was explicitly ruled out as extra infra scope; the app can only guarantee correctness given a shared path | y |
| Max upload size | 10 MB | User-confirmed bound; prevents disk/memory abuse from an owner-only endpoint while covering larger raster logos | y |
| Allowed MIME types | `image/png`, `image/svg+xml` | Matches the existing frontend `<input accept="image/png,image/svg+xml">` (`SettingsPage.tsx:87`) - no new client contract needed | y (already implied by shipped UI) |
| Logo file naming | Fixed name (`logo` + extension of the upload), old file removed/overwritten on each successful upload | Singleton resource - avoids orphaned files accumulating in the mounted volume with no cleanup job | n - default chosen for simplicity |
| Access control on GET/PATCH | Both restricted to `role = owner` (`RequireRole(db.RoleOwner)`, i.e. the existing `ownerOnly` group in `routes.go:50`) | `SettingsPage.tsx:70` already ships the copy "Visível apenas para Owners" - the whole page, not just editing, is owner-gated | y (already committed to in shipped UI copy) |
| Public exposure path | No new public endpoint; `publicStatusResponse` (shared by the production Host-routed handler and the I12 dev/preview handler) gains a `company` field sourced from `CompanySettingsRepository.Get` | Avoids a second unauthenticated surface; both existing public handlers already share one `composeResponse` (`public_status_handler.go:122`) | y |
| Fresh-install default row | A migration seeds exactly one `company_settings` row (empty name/contact_email, null logo_url) via `INSERT ... ON CONFLICT DO NOTHING` | Keeps `Get` a plain `SELECT` with no "row missing" branch to test/handle, consistent with treating this as a true singleton | y |
| Concurrent PATCH requests | Last-write-wins, no optimistic locking/versioning | No concurrent-edit requirement stated; owner-only + low-frequency admin action makes lost-update risk negligible | y (default, revisit only if reported as a real problem) |

**Open questions:** none - all resolved or logged above.

---

## User Stories

### P1: Owner edits company identity (name + contact e-mail) ⭐ MVP

**User Story**: As an owner, I want to set my company's name and contact e-mail so that the admin panel and public status pages reflect real company identity instead of placeholder mock data.

**Why P1**: Core of the feature - the SettingsPage form already exists and calls a real API shape (`hooks.ts`); today it silently talks to nothing real.

**Acceptance Criteria**:

1. WHEN an owner submits `PATCH /api/company-settings` with a non-empty `name` and a syntactically valid `contact_email` THEN the system SHALL persist both values and respond `200` with the updated `CompanySettings` JSON.
2. WHEN any admin (any role) with a valid session calls `GET /api/company-settings` without the `owner` role THEN the system SHALL respond `403 Forbidden`. <!-- unwanted-behavior, RBAC boundary -->
3. WHEN an owner calls `GET /api/company-settings` on a fresh install (no prior PATCH ever issued) THEN the system SHALL respond `200` with the seeded row (`name: ""`, `contact_email: ""`, `logo_url: null`), never `404`.
4. IF `PATCH /api/company-settings` body has an empty `name` THEN the system SHALL respond `422` and SHALL NOT modify the persisted row.
5. IF `PATCH /api/company-settings` body has a `contact_email` that fails e-mail-format validation THEN the system SHALL respond `422` and SHALL NOT modify the persisted row.
6. The system SHALL enforce exactly one `company_settings` row at all times (seeded once at migration time, never created or deleted afterward).

**Independent Test**: Log in as owner, edit name/e-mail in `SettingsPage`, reload the page (or call `GET` directly) - the new values persist and survive a backend restart.

---

### P1: Owner uploads/replaces the company logo ⭐ MVP

**User Story**: As an owner, I want to upload a logo image that keeps working after the app restarts or a Kubernetes pod is rescheduled, so the branding isn't silently lost in production.

**Why P1**: `SettingsPage` already ships the upload UI (`SettingsPage.tsx:84-95`); shipping name/e-mail without it would leave a visibly broken control in the same form.

**Acceptance Criteria**:

1. WHEN an owner uploads a file of MIME type `image/png` or `image/svg+xml` no larger than 10 MB THEN the system SHALL store it under the configured uploads directory, update `logo_url` to a path the app itself serves, and respond `200` with the updated `CompanySettings` JSON.
2. IF the uploaded file exceeds 10 MB THEN the system SHALL respond `422`, SHALL NOT write any file, and SHALL NOT modify the previously stored `logo_url`.
3. IF the uploaded file's MIME type is neither `image/png` nor `image/svg+xml` THEN the system SHALL respond `422` and SHALL NOT modify the previously stored logo.
4. WHEN a new logo upload succeeds THEN the system SHALL remove or overwrite the previously stored logo file so the uploads directory never accumulates more than one logo file at a time.
5. The system SHALL read the uploads directory path from the `UPLOADS_DIR` environment variable, defaulting to `./data/uploads` when unset, so operators can bind-mount a persistent volume onto that path in Docker/Kubernetes.
6. WHEN a stored logo file is requested at the path returned in `logo_url` THEN the system SHALL serve its bytes with no authentication required (the public status page must be able to render it unauthenticated).
7. IF writing the uploaded file to `UPLOADS_DIR` fails (e.g., disk full, permission denied) THEN the system SHALL respond `500`, SHALL NOT update `logo_url` in the database, and SHALL log the underlying error.
8. WHILE no logo has ever been uploaded, `GET /api/company-settings` SHALL return `logo_url: null`.

**Independent Test**: Upload a PNG through `SettingsPage`, confirm it renders immediately, restart the backend process (simulating a pod restart) with the same `UPLOADS_DIR` mount, and confirm `GET /api/company-settings` still returns the same `logo_url` and the file still serves.

---

### P2: Public status pages show the real company identity

**User Story**: As a visitor of a public status page (production or dev/preview), I want to see the real company name and logo, not a hardcoded placeholder, so the page looks legitimate.

**Why P2**: Builds directly on P1's persisted data; independently demoable and shippable slightly after P1 without blocking it.

**Acceptance Criteria**:

1. WHEN the production Host-routed public status endpoint or the I12 dev/preview-by-ID endpoint composes its response THEN the system SHALL include the current `company_settings.name` and `company_settings.logo_url` in the response, replacing the `mockData.companySettings` values used today.
2. WHILE `company_settings.logo_url` is `null` (no logo ever uploaded) the public response SHALL carry `logo_url: null` rather than a broken path or a placeholder image URL.

**Independent Test**: Set a company name/logo via `SettingsPage`, open a published status page (or the dev preview by ID) unauthenticated, and confirm the header shows the real name/logo instead of "Sua Empresa Ltda.".

---

## Edge Cases

- IF a PATCH and a concurrent logo upload race THEN the last write to each independent field wins (no cross-field locking) - accepted per Assumptions table.
- IF `UPLOADS_DIR` does not exist at startup THEN the system SHALL create it on first use (lazy `MkdirAll`) rather than failing process startup, since an owner may never upload a logo at all.
- IF the stored logo file is deleted or missing from disk (volume misconfigured post-restart) while `logo_url` still points to it THEN requesting that path SHALL respond `404`, and the admin panel SHALL surface the resulting broken `<img>` the same way any missing asset would (no special-cased recovery logic beyond that).

---

## Requirement Traceability

| Requirement ID | Story                                     | Phase  | Status  |
| --------------- | ------------------------------------------ | ------ | ------- |
| SET-01          | P1: Owner edits company identity          | Design | Implementing |
| SET-02          | P1: Owner edits company identity (RBAC)   | Design | Pending |
| SET-03          | P1: Owner edits company identity (seed)   | Design | Implementing |
| SET-04          | P1: Owner edits company identity (validation) | Design | Implementing |
| SET-05          | P1: Owner edits company identity (validation) | Design | Implementing |
| SET-06          | P1: Owner edits company identity (singleton) | Design | Implementing |
| SET-07          | P1: Owner uploads logo                    | Design | Pending |
| SET-08          | P1: Owner uploads logo (size limit)       | Design | Pending |
| SET-09          | P1: Owner uploads logo (MIME whitelist)   | Design | Pending |
| SET-10          | P1: Owner uploads logo (overwrite)        | Design | Implementing |
| SET-11          | P1: Owner uploads logo (config path)      | Design | Implementing |
| SET-12          | P1: Owner uploads logo (public serving)   | Design | Pending |
| SET-13          | P1: Owner uploads logo (write failure)    | Design | Pending |
| SET-14          | P1: Owner uploads logo (null default)     | Design | Pending |
| SET-15          | P2: Public pages show real identity       | Design | Pending |
| SET-16          | P2: Public pages show real identity (null logo) | Design | Pending |

**ID format:** `SET-[NUMBER]`

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 16 total, 0 mapped to tasks, 16 unmapped ⚠️ (expected at this stage - Tasks phase not yet run)

---

## Success Criteria

- [ ] `SettingsPage` reads and writes real data via `GET`/`PATCH /api/company-settings`; `mockData.companySettings` is no longer imported by any production code path.
- [ ] A logo uploaded through the admin panel is still served correctly after a backend process restart with the same `UPLOADS_DIR` volume mounted.
- [ ] Public status page (production and I12 dev/preview) shows the real company name/logo with zero references to `mockData.companySettings` remaining in `public-status/hooks.ts`.
