// Fixtures used by tests and by the dev-only "Viewing as" role selector
// (AuthProvider.setDevRole) - this is no longer the network layer (I6
// replaced apiClient.ts with real fetch). Types come from src/types/api.ts
// (I9); this file only declares the sample data.
// Contracts follow `design.md` § Data Models; where the UI needs something
// the design docs describe as "not returned by the API on read" (e.g.
// `service_ids` read back on an Incident), the fixture includes the field
// anyway to make the screen usable in dev/test.

import type {
  Admin,
  AdminInvite,
  IntegrationStatus,
  SLOSummary,
  Service,
  Domain,
  StatusPage,
  Incident,
  IncidentUpdate,
  PollerStatusEntry,
  CompanySettings,
  SessionView,
} from "../types/api";

interface AdminSeed extends Admin {
  password: string;
}

// MockSession is the mock-internal seed shape: it adds the fields the
// MSW layer needs to emulate backend filtering/state (row ownership and
// revocation), which the real GET /api/auth/sessions response never
// includes. The handler projects these rows back down to the exact
// SessionView shape before responding (AGENTS.md §5).
export interface MockSession extends SessionView {
  user_id: string;
  revoked_at: string | null;
}

export const admins: AdminSeed[] = [
  {
    id: "admin-1",
    email: "owner@vane.app",
    password: "demo1234",
    name: "Ana Owner",
    role: "owner",
    status: "active",
    last_access: new Date(Date.now() - 1000 * 60 * 2).toISOString(),
  },
  {
    id: "admin-2",
    email: "operator@vane.app",
    password: "demo1234",
    name: "Bruno Operator",
    role: "operator",
    status: "active",
    last_access: new Date(Date.now() - 1000 * 60 * 60 * 3).toISOString(),
  },
  {
    id: "admin-3",
    email: "viewer@vane.app",
    password: "demo1234",
    name: "Carla Viewer",
    role: "viewer",
    status: "active",
    last_access: new Date(Date.now() - 1000 * 60 * 60 * 24).toISOString(),
  },
];

export function findAdminByEmail(email: string): AdminSeed | undefined {
  return admins.find((a) => a.email.toLowerCase() === email.toLowerCase());
}

export function toPublicAdmin(admin: AdminSeed): Admin {
  return {
    id: admin.id,
    email: admin.email,
    name: admin.name,
    role: admin.role,
    status: admin.status,
    last_access: admin.last_access,
  };
}

// -- Convites pendentes de admin (AF-38: mesclados em GET /api/admins) --------

export const adminInvites: AdminInvite[] = [
  {
    id: "invite-1",
    email: "novo-operador@vane.app",
    role: "operator",
    status: "pending",
    expires_at: new Date(Date.now() + 1000 * 60 * 60 * 24 * 3).toISOString(),
  },
];

// -- Datadog integration --------------------------------------------------------

export const datadogIntegration: { connected: boolean } & IntegrationStatus = {
  connected: true,
  status: "active",
  last_checked_at: new Date(Date.now() - 1000 * 60 * 5).toISOString(),
  last_error: null,
};

export const sloCatalog: SLOSummary[] = [
  { id: "slo-1", name: "API disponibilidade 99.9%" },
  { id: "slo-2", name: "Checkout latência p95" },
  { id: "slo-3", name: "Autenticação disponibilidade" },
  { id: "slo-4", name: "Fila de notificações" },
];

// -- Services -------------------------------------------------------------------

// uptime_30d/last_seen_at are always null in the seed rows below - the MSW
// handler (toServiceResponse -> serviceUptimeAndLastSeen) computes the
// actual response values dynamically from current_status, so these are
// just placeholders satisfying the Service type.
export const services: Service[] = [
  {
    id: "svc-1",
    name: "API pública",
    slo_id: "slo-1",
    slo_name: "API disponibilidade 99.9%",
    monitor_mode: "slo",
    poll_type: null,
    poll_target: null,
    poll_interval_seconds: null,
    current_status: "operational",
    last_status_change_at: new Date(Date.now() - 1000 * 60 * 60 * 6).toISOString(),
    uptime_30d: null,
    last_seen_at: null,
  },
  {
    id: "svc-2",
    name: "Checkout",
    slo_id: "slo-2",
    slo_name: "Checkout latência p95",
    monitor_mode: "slo",
    poll_type: null,
    poll_target: null,
    poll_interval_seconds: null,
    current_status: "degraded",
    last_status_change_at: new Date(Date.now() - 1000 * 60 * 30).toISOString(),
    uptime_30d: null,
    last_seen_at: null,
  },
  {
    // slo_id/slo_name non-null (SPEC_DEVIATION, I15): the real services
    // table has slo_id NOT NULL (0004_services.up.sql) - a service always
    // has an SLO linked at creation, "not_configured" only means the
    // poller hasn't fetched a status for it yet, never "no SLO at all".
    id: "svc-3",
    name: "Notificações",
    slo_id: "slo-4",
    slo_name: "Fila de notificações",
    monitor_mode: "slo",
    poll_type: null,
    poll_target: null,
    poll_interval_seconds: null,
    current_status: "not_configured",
    last_status_change_at: new Date(Date.now() - 1000 * 60 * 60 * 24).toISOString(),
    uptime_30d: null,
    last_seen_at: null,
  },
  {
    id: "svc-4",
    name: "Fila de processamento",
    slo_id: "slo-3",
    slo_name: "Autenticação disponibilidade",
    monitor_mode: "slo",
    poll_type: null,
    poll_target: null,
    poll_interval_seconds: null,
    current_status: "operational",
    last_status_change_at: new Date(Date.now() - 1000 * 60 * 60 * 12).toISOString(),
    uptime_30d: null,
    last_seen_at: null,
  },
  {
    // Polling-manual fixture (manual-polling-monitoring T9): no slo_id/
    // slo_name at all - the list/detail subtitle fallback must show
    // poll_target instead, never blank/undefined.
    id: "svc-5",
    name: "Cache interno",
    slo_id: null,
    slo_name: null,
    monitor_mode: "polling",
    poll_type: "tcp",
    poll_target: "cache.acme.health:6379",
    poll_interval_seconds: 60,
    current_status: "operational",
    last_status_change_at: new Date(Date.now() - 1000 * 60 * 45).toISOString(),
    uptime_30d: null,
    last_seen_at: null,
  },
];

// -- Domains ---------------------------------------------------------------

export const domains: Domain[] = [
  {
    id: "dom-1",
    hostname: "status.acme.com",
    created_at: new Date(Date.now() - 1000 * 60 * 60 * 24 * 10).toISOString(),
    domain_type: "custom",
    status: "verified",
    ssl_status: "active",
    verified_at: new Date(Date.now() - 1000 * 60 * 60 * 24 * 9).toISOString(),
    last_error: null,
    attached_page_name: "Status Acme",
    attached_page_count: 1,
  },
  {
    id: "dom-2",
    hostname: "status.beta.io",
    created_at: new Date(Date.now() - 1000 * 60 * 60 * 24 * 2).toISOString(),
    domain_type: "custom",
    status: "pending",
    ssl_status: "pending",
    verified_at: null,
    last_error: null,
    attached_page_name: null,
    attached_page_count: 0,
  },
];

// -- Status Pages -------------------------------------------------------------

export const statusPages: StatusPage[] = [
  {
    id: "sp-1",
    name: "Status Acme",
    subdomain: "status",
    domain_id: "dom-1",
    state: "published",
    tls_last_error: null,
    created_at: new Date(Date.now() - 1000 * 60 * 60 * 24 * 9).toISOString(),
    service_ids: ["svc-1", "svc-2"],
  },
  {
    id: "sp-2",
    name: "Status Beta",
    subdomain: "status",
    domain_id: "dom-2",
    state: "pending_tls",
    tls_last_error: null,
    created_at: new Date(Date.now() - 1000 * 60 * 5).toISOString(),
    service_ids: ["svc-3"],
  },
  {
    id: "sp-3",
    name: "Status Gamma",
    subdomain: "status",
    domain_id: "dom-2",
    state: "tls_failed",
    tls_last_error: "Falha ao validar propriedade do domínio via DNS-01.",
    created_at: new Date(Date.now() - 1000 * 60 * 60 * 24).toISOString(),
    service_ids: [],
  },
  {
    id: "sp-4",
    name: "Status Delta",
    subdomain: "status-delta",
    domain_id: "dom-1",
    state: "published",
    tls_last_error: null,
    created_at: new Date(Date.now() - 1000 * 60 * 60 * 24 * 20).toISOString(),
    service_ids: ["svc-4"],
  },
];

// Counter simulating how many times a status page in issuance has already been
// polled — used only so the mock advances on its own to a terminal state after
// a few polling requests, simulating the real behavior of TLS issuance.
const statusPagePollCount = new Map<string, number>();

export function advanceStatusPagePolling(page: StatusPage): StatusPage {
  if (page.state !== "draft") return page;
  const count = (statusPagePollCount.get(page.id) ?? 0) + 1;
  statusPagePollCount.set(page.id, count);
  if (count >= 3) {
    page.state = "published";
  }
  return page;
}

// -- Incidentes -----------------------------------------------------------------

export const incidents: Incident[] = [
  {
    // Auto-created (AI-12) with a pending closing-comment proposal
    // (AI-19/AI-20/AI-21/AI-22) still awaiting confirm/discard - exercises
    // both the auto_created badge and the pending-close-comment banner in
    // the admin dashboard, plus description rendering on the public page.
    id: "inc-1",
    title: "Latência elevada no Checkout",
    status: "monitoring",
    created_at: new Date(Date.now() - 1000 * 60 * 60 * 2).toISOString(),
    resolved_at: null,
    service_ids: ["svc-2"],
    description: "Aumento sustentado de latência p95 no Checkout, acima do SLO configurado.",
    pending_close_comment: "Latência normalizada após rollback do deploy; monitorando estabilização.",
    auto_created: true,
    severity: "critical",
  },
  {
    // Manually created, no description/proposal - exercises the
    // title-only/no-banner fallback paths.
    id: "inc-2",
    title: "Indisponibilidade parcial da API",
    status: "resolved",
    created_at: new Date(Date.now() - 1000 * 60 * 60 * 24 * 3).toISOString(),
    resolved_at: new Date(Date.now() - 1000 * 60 * 60 * 24 * 3 + 1000 * 60 * 45).toISOString(),
    service_ids: ["svc-1"],
    description: null,
    pending_close_comment: null,
    auto_created: false,
    severity: "moderate",
  },
];

export const incidentUpdates: IncidentUpdate[] = [
  {
    id: "upd-1",
    incident_id: "inc-1",
    body: "Identificamos aumento de latência no serviço de Checkout e estamos investigando.",
    created_at: new Date(Date.now() - 1000 * 60 * 60 * 2).toISOString(),
    author_id: "admin-1",
    is_ai_summary: false,
  },
  {
    id: "upd-2",
    incident_id: "inc-1",
    body: "Causa raiz identificada: pico de tráfego não previsto. Monitorando estabilização.",
    created_at: new Date(Date.now() - 1000 * 60 * 90).toISOString(),
    author_id: "admin-1",
    is_ai_summary: false,
  },
  {
    id: "upd-3",
    incident_id: "inc-2",
    body: "API pública apresentou erros 5xx intermitentes por cerca de 45 minutos.",
    created_at: new Date(Date.now() - 1000 * 60 * 60 * 24 * 3).toISOString(),
    author_id: "admin-1",
    is_ai_summary: false,
  },
  {
    id: "upd-4",
    incident_id: "inc-2",
    body: "Incidente resolvido após rollback do deploy problemático.",
    created_at: new Date(Date.now() - 1000 * 60 * 60 * 24 * 3 + 1000 * 60 * 45).toISOString(),
    author_id: null,
    is_ai_summary: true,
  },
];

// -- Poller ---------------------------------------------------------------------

export const pollerStatus: PollerStatusEntry[] = [
  {
    provider: "datadog",
    status: "active",
    last_checked_at: new Date(Date.now() - 1000 * 60 * 2).toISOString(),
    last_error: null,
  },
];

// Poller leadership/activity fixture (poller-status-page POLLPG-01..05) -
// mirrors PollerStatusHandler.List's non-list fields. Mutable per-test via
// server.use overrides in MSW handlers; this default reflects a healthy
// single-replica install (leader elected, poller running).
export const pollerLeadership: {
  leader_elected: boolean;
  poller_running: boolean;
  replica: { application_name: string; backend_start: string } | null;
  checks_last_minute: number;
} = {
  leader_elected: true,
  poller_running: true,
  replica: { application_name: "vane-0", backend_start: new Date(Date.now() - 1000 * 60 * 30).toISOString() },
  checks_last_minute: 4,
};

// -- Company settings -----------------------------------------------------

export const companySettings: CompanySettings = {
  name: "Sua Empresa Ltda.",
  contact_email: "contato@suaempresa.com",
  logo_url: null,
  locale: "pt-BR",
};

// -- Helpers de id ---------------------------------------------------------------

let idCounter = 100;
export function nextId(prefix: string): string {
  idCounter += 1;
  return `${prefix}-${idCounter}`;
}

// -- Active sessions (per-device, user-sessions spec) ---------------------------
//
// Seeding strategy (deterministic so tests can rely on it):
//   - sess-1 + sess-2 belong to admin-1 (the most-used test user; lets
//     list-filter + cross-session revoke tests run without seeding).
//   - sess-3 belongs to admin-2 (lets the "DELETE another user's session
//     returns 404" anti-enumeration case run without seeding).
//   - admin-3 has no sessions (lets the "empty list" rendering case be
//     asserted after a `server.use` override that removes the other two).
// `current` is set to false in the seed; the MSW handler sets it true
// at request time on the row whose id matches the current session id,
// matching how the real backend's sessionsHandler.List derives it from
// the JWT sid claim.

export const sessions: MockSession[] = [
  {
    id: "sess-1",
    user_id: "admin-1",
    user_agent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36",
    ip: "10.0.0.42",
    created_at: new Date(Date.now() - 1000 * 60 * 60 * 2).toISOString(),
    last_seen_at: new Date(Date.now() - 1000 * 60 * 5).toISOString(),
    revoked_at: null,
    current: false,
  },
  {
    id: "sess-2",
    user_id: "admin-1",
    user_agent: "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1",
    ip: "10.0.0.99",
    created_at: new Date(Date.now() - 1000 * 60 * 60 * 24).toISOString(),
    last_seen_at: new Date(Date.now() - 1000 * 60 * 30).toISOString(),
    revoked_at: null,
    current: false,
  },
  {
    id: "sess-3",
    user_id: "admin-2",
    user_agent: "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36",
    ip: "192.168.1.10",
    created_at: new Date(Date.now() - 1000 * 60 * 60 * 6).toISOString(),
    last_seen_at: new Date(Date.now() - 1000 * 60 * 60).toISOString(),
    revoked_at: null,
    current: false,
  },
];
