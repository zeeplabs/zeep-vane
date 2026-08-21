// Fixtures compartilhadas pela camada mock de API (src/lib/apiClient.ts).
// Arrays mutáveis em memória — simulam um backend real durante a sessão do browser.
// SPEC_DEVIATION: dado 100% fake, sem rede real (ver AD-006 em .specs/STATE.md).
// Contratos seguem `design.md` § Data Models; onde a UI precisa de algo que o
// design documenta como "a API não retorna na leitura" (ex.: `service_ids` no
// Incident lido de volta), o mock inclui o campo mesmo assim para viabilizar a
// tela — reconciliar com o contrato real do backend na fase de integração.

export type Role = "owner" | "operator" | "viewer";

export interface Admin {
  id: string;
  email: string;
  role: Role;
  status: "active" | "pending";
}

interface AdminSeed extends Admin {
  password: string;
}

export const admins: AdminSeed[] = [
  { id: "admin-1", email: "owner@vane.app", password: "demo1234", role: "owner", status: "active" },
  {
    id: "admin-2",
    email: "operator@vane.app",
    password: "demo1234",
    role: "operator",
    status: "active",
  },
  { id: "admin-3", email: "viewer@vane.app", password: "demo1234", role: "viewer", status: "active" },
];

export function findAdminByEmail(email: string): AdminSeed | undefined {
  return admins.find((a) => a.email.toLowerCase() === email.toLowerCase());
}

export function toPublicAdmin(admin: AdminSeed): Admin {
  return { id: admin.id, email: admin.email, role: admin.role, status: admin.status };
}

// -- Convites pendentes de admin (AF-38: mesclados em GET /api/admins) --------

export interface AdminInvite {
  id: string;
  email: string;
  role: Role;
  status: "pending";
  expires_at: string;
}

export const adminInvites: AdminInvite[] = [
  {
    id: "invite-1",
    email: "novo-operador@vane.app",
    role: "operator",
    status: "pending",
    expires_at: new Date(Date.now() + 1000 * 60 * 60 * 24 * 3).toISOString(),
  },
];

// -- Integração Datadog --------------------------------------------------------

export interface IntegrationStatus {
  status: "active" | "invalid";
  last_checked_at: string | null;
  last_error: string | null;
}

export const datadogIntegration: { connected: boolean } & IntegrationStatus = {
  connected: true,
  status: "active",
  last_checked_at: new Date(Date.now() - 1000 * 60 * 5).toISOString(),
  last_error: null,
};

export interface SLOSummary {
  id: string;
  name: string;
}

export const sloCatalog: SLOSummary[] = [
  { id: "slo-1", name: "API disponibilidade 99.9%" },
  { id: "slo-2", name: "Checkout latência p95" },
  { id: "slo-3", name: "Autenticação disponibilidade" },
  { id: "slo-4", name: "Fila de notificações" },
];

// -- Serviços -------------------------------------------------------------------

export type ServiceStatus = "not_configured" | "operational" | "degraded" | "outage";

export interface Service {
  id: string;
  name: string;
  slo_id: string | null;
  slo_name: string | null;
  current_status: ServiceStatus;
  last_status_change_at: string;
}

export const services: Service[] = [
  {
    id: "svc-1",
    name: "API pública",
    slo_id: "slo-1",
    slo_name: "API disponibilidade 99.9%",
    current_status: "operational",
    last_status_change_at: new Date(Date.now() - 1000 * 60 * 60 * 6).toISOString(),
  },
  {
    id: "svc-2",
    name: "Checkout",
    slo_id: "slo-2",
    slo_name: "Checkout latência p95",
    current_status: "degraded",
    last_status_change_at: new Date(Date.now() - 1000 * 60 * 30).toISOString(),
  },
  {
    id: "svc-3",
    name: "Notificações",
    slo_id: null,
    slo_name: null,
    current_status: "not_configured",
    last_status_change_at: new Date(Date.now() - 1000 * 60 * 60 * 24).toISOString(),
  },
];

// -- Domínios ---------------------------------------------------------------

export interface Domain {
  id: string;
  hostname: string;
  created_at: string;
}

export const domains: Domain[] = [
  { id: "dom-1", hostname: "status.acme.com", created_at: new Date(Date.now() - 1000 * 60 * 60 * 24 * 10).toISOString() },
  { id: "dom-2", hostname: "status.beta.io", created_at: new Date(Date.now() - 1000 * 60 * 60 * 24 * 2).toISOString() },
];

// -- Status Pages -------------------------------------------------------------

export type StatusPageState = "draft" | "published" | "tls_failed";

export interface StatusPage {
  id: string;
  name: string;
  subdomain: string;
  domain_id: string;
  state: StatusPageState;
  tls_last_error: string | null;
  created_at: string;
  service_ids: string[];
}

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
    state: "draft",
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
];

// Contador simulando quantas vezes um status page em emissão já foi consultado —
// usado só para o mock avançar sozinho pra um estado terminal após algumas
// consultas de polling, simulando o comportamento real de emissão de TLS.
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

export const incidents: Incident[] = [
  {
    id: "inc-1",
    title: "Latência elevada no Checkout",
    status: "monitoring",
    created_at: new Date(Date.now() - 1000 * 60 * 60 * 2).toISOString(),
    resolved_at: null,
    service_ids: ["svc-2"],
  },
  {
    id: "inc-2",
    title: "Indisponibilidade parcial da API",
    status: "resolved",
    created_at: new Date(Date.now() - 1000 * 60 * 60 * 24 * 3).toISOString(),
    resolved_at: new Date(Date.now() - 1000 * 60 * 60 * 24 * 3 + 1000 * 60 * 45).toISOString(),
    service_ids: ["svc-1"],
  },
];

export const incidentUpdates: IncidentUpdate[] = [
  {
    id: "upd-1",
    incident_id: "inc-1",
    body: "Identificamos aumento de latência no serviço de Checkout e estamos investigando.",
    created_at: new Date(Date.now() - 1000 * 60 * 60 * 2).toISOString(),
  },
  {
    id: "upd-2",
    incident_id: "inc-1",
    body: "Causa raiz identificada: pico de tráfego não previsto. Monitorando estabilização.",
    created_at: new Date(Date.now() - 1000 * 60 * 90).toISOString(),
  },
  {
    id: "upd-3",
    incident_id: "inc-2",
    body: "API pública apresentou erros 5xx intermitentes por cerca de 45 minutos.",
    created_at: new Date(Date.now() - 1000 * 60 * 60 * 24 * 3).toISOString(),
  },
  {
    id: "upd-4",
    incident_id: "inc-2",
    body: "Incidente resolvido após rollback do deploy problemático.",
    created_at: new Date(Date.now() - 1000 * 60 * 60 * 24 * 3 + 1000 * 60 * 45).toISOString(),
  },
];

// -- Poller ---------------------------------------------------------------------

export interface PollerStatusEntry {
  provider: string;
  status: string;
  last_checked_at: string | null;
  last_error: string | null;
}

export const pollerStatus: PollerStatusEntry[] = [
  {
    provider: "datadog",
    status: "active",
    last_checked_at: new Date(Date.now() - 1000 * 60 * 2).toISOString(),
    last_error: null,
  },
];

// -- Helpers de id ---------------------------------------------------------------

let idCounter = 100;
export function nextId(prefix: string): string {
  idCounter += 1;
  return `${prefix}-${idCounter}`;
}
