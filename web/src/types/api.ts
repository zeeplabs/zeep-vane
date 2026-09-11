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
}

export type ServiceStatus = "not_configured" | "operational" | "degraded" | "outage";

export interface Service {
  id: string;
  name: string;
  slo_id: string | null;
  slo_name: string | null;
  current_status: ServiceStatus;
  last_status_change_at: string;
}

export interface Domain {
  id: string;
  hostname: string;
  created_at: string;
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
}

export interface IncidentUpdate {
  id: string;
  incident_id: string;
  body: string;
  created_at: string;
}

export interface PollerStatusEntry {
  provider: string;
  status: string;
  last_checked_at: string | null;
  last_error: string | null;
}

// TaxIDType is CompanySettings.tax_id_type's allowed values (multi-tenancy-
// core P2, TENANT-22/23) - CPF for a person, CNPJ for a company; both
// Brazilian tax id formats, backend-validated by digit count
// (db.ErrInvalidTaxID: 11 for cpf, 14 for cnpj).
export type TaxIDType = "cpf" | "cnpj";

export interface CompanySettings {
  name: string;
  contact_email: string;
  logo_url: string | null;
  // legal_name/tax_id/tax_id_type are all optional (TENANT-22) -
  // billing_address is deliberately absent from this type: the backend
  // never returns it from this endpoint (TENANT-24, SaaS billing feature
  // owns its exposure).
  legal_name?: string | null;
  tax_id?: string | null;
  tax_id_type?: TaxIDType | null;
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
export interface SessionView {
  id: string;
  user_id: string;
  user_agent: string | null;
  ip: string | null;
  created_at: string;
  last_seen_at: string | null;
  expires_at: string;
  revoked_at: string | null;
  // current is true when this row's id matches the JWT's sid claim
  // (i.e. this is the session the request is being made from). The real
  // backend sets it in sessionsHandler.List by comparing to the sid the
  // RequireAuth middleware put in the request context; the field is not
  // stored in the row.
  current: boolean;
}
