// Shapes of the real backend's API contracts (design.md § Data Models).
// This is the source of truth for types across the app - src/lib/mockData.ts
// only holds fixture data now, it doesn't originate these types.

export type Role = "owner" | "operator" | "viewer";

// TenantMembership is one entry in GET /api/auth/me's memberships list -
// a tenant the authenticated user belongs to, and their role in it
// (multi-tenancy-core, TENANT-19/20/21).
export interface TenantMembership {
  tenant_id: string;
  role: Role;
  name: string; // tenant display name (new-layout-migration, SHELL-20)
  plan_tier: string; // tenant plan, "" if unset (new-layout-migration, SHELL-21)
}

export interface Admin {
  id: string;
  email: string;
  name?: string;
  phone?: string;
  role: Role;
  status: "active" | "pending";
  // last_access is users-page USRPG-05's "Last access" - the most
  // recent sessions.last_seen_at for this user, null when they never had
  // a session (pending invite, or an active member who never logged in).
  last_access?: string | null;
}

export interface AdminInvite {
  id: string;
  email: string;
  name?: string;
  phone?: string;
  role: Role;
  status: "pending";
  expires_at: string;
}

export interface IntegrationStatus {
  status: "active" | "invalid";
  last_checked_at: string | null;
  last_error: string | null;
}

export interface SLOSummary {
  id: string;
  name: string;
  // slo_type/datadog_service_tag ride along with every search result
  // (slo-root-cause-enrichment RCA-01) - "" for datadog_service_tag when
  // the SLO has 0 or 2+ service_tags entries (a flow-type SLO), same
  // ""-means-absent convention as the rest of this feature.
  slo_type: string;
  datadog_service_tag: string;
}

export type ServiceStatus = "not_configured" | "operational" | "degraded" | "outage";

// MonitorMode/PollType mirror the backend's manual-polling-monitoring
// contract (internal/api/services_handler.go's createServiceRequest/
// serviceResponse) - "slo" (Datadog SLO-backed, today's only mode) or
// "polling" (direct HTTP(S)/TCP/Ping check, no Datadog SLO involved).
export type MonitorMode = "slo" | "polling";
export type PollType = "http" | "tcp" | "ping";

export interface Service {
  id: string;
  name: string;
  slo_id: string | null;
  slo_name: string | null;
  // monitor_mode defaults to "slo" server-side when omitted from a create
  // request, but every service the API returns always carries an explicit
  // value (never optional on read).
  monitor_mode: MonitorMode;
  // poll_type/poll_target/poll_interval_seconds are null for
  // monitor_mode="slo", all three set together for monitor_mode="polling"
  // (services_monitor_mode_fields_check, 0034_service_polling_mode).
  poll_type: PollType | null;
  poll_target: string | null;
  poll_interval_seconds: number | null;
  current_status: ServiceStatus;
  last_status_change_at: string;
  // uptime_30d/last_seen_at (monitored-services-page SVC-01/SVC-06): null
  // when the service has no StatusInterval data yet ("—" in the UI), a
  // real value once the poller has run at least once.
  uptime_30d: number | null;
  last_seen_at: string | null;
}

// HourlyBucket is one bar of the detail drawer's 24-hour status history
// strip (SVC-17), mirroring internal/api's hourlyBucketResponse -
// "no_data" is a real per-bucket value distinct from the overall
// ServiceStatus vocabulary (design.md § Data Models Relationships).
export interface HourlyBucket {
  start: string;
  status: "operational" | "degraded" | "outage" | "no_data";
}

// ServiceDetail mirrors GET /api/services/{id} (design.md, flat DTO, not
// Page<T>) - the monitored-services-page detail drawer's read.
export interface ServiceDetail extends Service {
  status_analysis: string | null;
  incidents_30d: number;
  hourly_buckets: HourlyBucket[];
}

// DomainType/DomainStatus/DomainSSLStatus mirror the backend's
// domain-verification-state contract (internal/api/domains_handler.go's
// domainResponse) - "custom" is the only domain_type value the backend
// ever produces today (domains-status-pages-page's Assumptions: "Vane
// subdomain" ships disabled/decorative, no backend support).
export type DomainType = "custom";
export type DomainStatus = "pending" | "verified" | "error";
export type DomainSSLStatus = "pending" | "active" | "error";

export interface Domain {
  id: string;
  hostname: string;
  created_at: string;
  domain_type: DomainType;
  status: DomainStatus;
  ssl_status: DomainSSLStatus;
  verified_at: string | null;
  last_error: string | null;
  // attached_page_name/attached_page_count are the read-side join over
  // status_pages.domain_id (domains-status-pages-page DSP-02/03/04) - null/0
  // when no status page is attached, the earliest-created attached page's
  // name and the full count otherwise.
  attached_page_name: string | null;
  attached_page_count: number;
}

export type StatusPageState = "draft" | "pending_tls" | "published" | "tls_failed";

export interface StatusPage {
  id: string;
  name: string;
  subdomain: string | null;
  domain_id: string | null;
  state: StatusPageState;
  tls_last_error: string | null;
  created_at: string;
  service_ids: string[];
}

export type IncidentStatus = "investigating" | "identified" | "monitoring" | "resolved";

export type IncidentSeverity = "minor" | "moderate" | "critical";

export interface Incident {
  id: string;
  title: string;
  status: IncidentStatus;
  created_at: string;
  resolved_at: string | null;
  service_ids: string[];
  // description, pending_close_comment, and auto_created are admin-only
  // fields (internal/api/incidents_handler.go's incidentResponse, AI-09,
  // AI-19/AI-20, AI-12) - never present on the public incident response.
  description: string | null;
  pending_close_comment: string | null;
  auto_created: boolean;
  // severity is required on every incident (INCSEV-01/02).
  severity: IncidentSeverity;
}

export interface IncidentUpdate {
  id: string;
  incident_id: string;
  body: string;
  created_at: string;
  // author_id/is_ai_summary attribute the entry to a human or the AI
  // closing-summary flow (INCSEV-05/06).
  author_id: string | null;
  is_ai_summary: boolean;
}

export interface PollerStatusEntry {
  provider: string;
  status: string;
  last_checked_at: string | null;
  last_error: string | null;
}

export interface PollerReplica {
  application_name: string;
  backend_start: string;
}

// Mirrors PollerStatusHandler.List's response shape
// (internal/api/poller_status.go) - leadership/activity fields sit
// alongside the paginated integrations list rather than nesting them
// under a generic Page<T> envelope, same convention as
// EmailProvidersResponse (AGENTS.md §4).
export interface PollerStatusResponse {
  leader_elected: boolean;
  poller_running: boolean;
  replica: PollerReplica | null;
  checks_last_minute: number;
  items: PollerStatusEntry[];
  total: number;
  page: number;
  page_size: number;
}

// TaxIDType is CompanySettings.tax_id_type's allowed values (multi-tenancy-
// core P2, TENANT-22/23) - CPF for a person, CNPJ for a company; both
// Brazilian tax id formats, backend-validated by digit count
// (db.ErrInvalidTaxID: 11 for cpf, 14 for cnpj).
export type TaxIDType = "cpf" | "cnpj";

// TenantBillingAddress is the settings-page fiscal address shape
// (CFGPG-06/07) - supersedes TENANT-24's decision to keep billing_address
// hidden from this endpoint.
export interface TenantBillingAddress {
  zip: string;
  street: string;
  number: string;
  complement: string;
  state: string;
  city: string;
  country: string;
}

export interface CompanySettings {
  name: string;
  contact_email: string;
  logo_url: string | null;
  // legal_name/tax_id/tax_id_type/website/timezone/billing_address are all
  // optional (TENANT-22, CFGPG-01/06).
  legal_name?: string | null;
  tax_id?: string | null;
  tax_id_type?: TaxIDType | null;
  website?: string | null;
  timezone?: string | null;
  locale: string;
  billing_address?: TenantBillingAddress | null;
}

// Page is the shared response envelope for every paginated list endpoint
// (design.md § Data Models), mirroring the backend's internal/api.Page[T].
export interface Page<T> {
  items: T[];
  total: number;
  page: number;
  page_size: number;
}

// SessionView mirrors internal/api/sessions_handler.go's SessionView
// (user-sessions spec). Returned by GET /api/auth/sessions as a flat
// list (not wrapped in Page<T> - per-device sessions are bounded by a
// single user's device count, typically 1-5 rows; pagination is not
// useful at that scale and would just add noise to the contract).
//
// Fields are exactly those the backend serializes (the nullable columns
// are `omitempty`, so they come back absent rather than null): id,
// user_agent?, ip?, created_at, last_seen_at?, current. The row's owner
// and revocation state are never returned - the endpoint is already
// scoped to the caller - so do not add user_id/revoked_at/expires_at
// here; that drift is what AGENTS.md §5 warns about.
export interface SessionView {
  id: string;
  user_agent: string | null;
  ip: string | null;
  created_at: string;
  last_seen_at: string | null;
  // current is true when this row's id matches the JWT's sid claim
  // (i.e. this is the session the request is being made from). The real
  // backend sets it in sessionsHandler.List by comparing to the sid the
  // RequireAuth middleware put in the request context; the field is not
  // stored in the row.
  current: boolean;
}

// OverviewResponse mirrors internal/api/overview_handler.go's
// OverviewResponse (dashboard-overview-page). Returned by GET /api/overview
// as a flat summary DTO, not a Page<T> envelope (the endpoint is not a
// paginated list). uptime_avg_30d and each bucket's uptime_percent are null
// when no service has data in the window - the UI renders "—" for null.
export interface OverviewResponse {
  uptime_avg_30d: number | null;
  uptime_avg_30d_prior: number | null;
  open_incidents: number;
  open_incidents_critical: number;
  open_incidents_monitoring: number;
  unhealthy_services: number;
  total_services: number;
  verified_domains: number;
  total_domains: number;
  uptime_series: OverviewUptimeBucket[];
  recent_incidents: OverviewIncident[];
}

// OverviewUptimeBucket is one day of the 14-day chart; date is the local
// (America/Sao_Paulo) calendar day, YYYY-MM-DD.
export interface OverviewUptimeBucket {
  date: string;
  uptime_percent: number | null;
}

// OverviewIncident is one row of the recent-incidents list (0-3 items).
export interface OverviewIncident {
  id: string;
  title: string;
  status: string;
  created_at: string;
}

// AuditLogEntry mirrors internal/api/audit_log_handler.go's
// auditLogEntryResponse (recent-team-activity, ACTIVITY-06/09). Returned
// by GET /api/audit-log as a plain array, not a Page<T> envelope - a
// fixed-size (<=20) summary feed, not a paginated list screen. target_label
// is null for historical pre-feature rows; actor_name is "" with
// actor_deleted true when the actor's user row was hard-deleted (the
// frontend substitutes a localized placeholder, never the raw empty
// string or a UUID).
export interface AuditLogEntry {
  action: string;
  target_label: string | null;
  actor_name: string;
  actor_deleted: boolean;
  created_at: string;
}
