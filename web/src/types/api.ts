// Shapes of the real backend's API contracts (design.md § Data Models).
// This is the source of truth for types across the app - src/lib/mockData.ts
// only holds fixture data now, it doesn't originate these types.

export type Role = "owner" | "operator" | "viewer";

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

export type StatusPageState = "draft" | "published" | "tls_failed";

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

export interface CompanySettings {
  name: string;
  contact_email: string;
  logo_url: string | null;
}

// Page is the shared response envelope for every paginated list endpoint
// (design.md § Data Models), mirroring the backend's internal/api.Page[T].
export interface Page<T> {
  items: T[];
  total: number;
  page: number;
  page_size: number;
}
