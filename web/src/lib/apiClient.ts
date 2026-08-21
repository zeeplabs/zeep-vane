// Camada mock: nenhuma chamada de rede real acontece aqui — resolve contra
// fixtures em memória (mockData.ts). Será substituída por fetch real na fase de integração.
// SPEC_DEVIATION: ver AD-006 em .specs/STATE.md — esta rodada de execução é
// frontend-only, mock-first; T1-T8 do backend real ainda não existem.
import {
  admins,
  adminInvites,
  findAdminByEmail,
  toPublicAdmin,
  nextId,
  domains,
  statusPages,
  advanceStatusPagePolling,
  services,
  incidents,
  incidentUpdates,
  datadogIntegration,
  sloCatalog,
  pollerStatus,
  type Admin,
  type Domain,
  type StatusPage,
  type Service,
  type Incident,
  type IncidentUpdate,
  type Role,
} from "./mockData";

export class ApiError extends Error {
  status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

type UnauthorizedHandler = () => void;

let unauthorizedHandler: UnauthorizedHandler | null = null;

export function setUnauthorizedHandler(fn: UnauthorizedHandler | null): void {
  unauthorizedHandler = fn;
}

/** Dispara o handler de 401 registrado. Exposto para testes/simulação manual
 * de expiração de sessão — não é chamado automaticamente por timeout. */
export function triggerUnauthorized(): void {
  unauthorizedHandler?.();
}

// "Sessão" simulada do backend mock: id do admin autenticado, se houver.
let currentSessionAdminId: string | null = null;

function currentAdmin(): Admin {
  const admin = admins.find((a) => a.id === currentSessionAdminId);
  if (!admin) throw new ApiError(401, "Não autenticado.");
  return toPublicAdmin(admin);
}

function requireOwner(): Admin {
  const admin = currentAdmin();
  if (admin.role !== "owner") throw new ApiError(403, "Acesso restrito ao papel owner.");
  return admin;
}

function delay(): Promise<void> {
  const ms = 150 + Math.floor(Math.random() * 250);
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function parseBody(init?: RequestInit): Record<string, unknown> {
  if (!init?.body) return {};
  try {
    return JSON.parse(init.body as string);
  } catch {
    return {};
  }
}

function maskKey(key: string): string {
  const last4 = key.slice(-4).padStart(4, "•");
  return `•••• •••• •••• ${last4}`;
}

function adminsWithPending(): (Admin & { expires_at?: string })[] {
  const active = admins.map((a) => toPublicAdmin(a));
  const pending = adminInvites.map((inv) => ({
    id: inv.id,
    email: inv.email,
    role: inv.role,
    status: "pending" as const,
    expires_at: inv.expires_at,
  }));
  return [...active, ...pending];
}

async function handleRoute(rawPath: string, init?: RequestInit): Promise<unknown> {
  const method = (init?.method ?? "GET").toUpperCase();
  const url = new URL(rawPath, "http://mock.local");
  const path = url.pathname;
  const body = parseBody(init);

  // -- Auth --------------------------------------------------------------

  if (method === "POST" && path === "/api/auth/login") {
    const email = String(body.email ?? "");
    const password = String(body.password ?? "");
    const admin = findAdminByEmail(email);
    if (!admin || admin.password !== password) {
      throw new ApiError(401, "E-mail ou senha inválidos.");
    }
    currentSessionAdminId = admin.id;
    return { admin: toPublicAdmin(admin), token: "mock-token-" + admin.id };
  }

  if (method === "POST" && path === "/api/auth/logout") {
    currentSessionAdminId = null;
    return {};
  }

  if (method === "GET" && path === "/api/auth/me") {
    return { admin: currentAdmin() };
  }

  // Toda rota abaixo exige sessão (equivalente a requireAuth no backend real).
  currentAdmin();

  // -- Integrações Datadog -------------------------------------------------

  if (method === "GET" && path === "/api/integrations/datadog") {
    if (!datadogIntegration.connected) {
      return { connected: false };
    }
    return {
      connected: true,
      status: datadogIntegration.status,
      last_checked_at: datadogIntegration.last_checked_at,
      last_error: datadogIntegration.last_error,
    };
  }

  if (method === "POST" && path === "/api/integrations/datadog") {
    const apiKey = String(body.api_key ?? "");
    const appKey = String(body.app_key ?? "");
    if (!apiKey || !appKey) {
      throw new ApiError(422, "Informe API key e App key.");
    }
    datadogIntegration.connected = true;
    datadogIntegration.status = "active";
    datadogIntegration.last_checked_at = new Date().toISOString();
    datadogIntegration.last_error = null;
    return {
      connected: true,
      status: "active",
      last_checked_at: datadogIntegration.last_checked_at,
      last_error: null,
      masked_key: maskKey(apiKey),
    };
  }

  if (method === "GET" && path === "/api/integrations/datadog/slos") {
    const query = (url.searchParams.get("query") ?? "").trim().toLowerCase();
    if (!query) return [];
    return sloCatalog.filter((slo) => slo.name.toLowerCase().includes(query));
  }

  // -- Serviços --------------------------------------------------------------

  if (method === "GET" && path === "/api/services") {
    return services;
  }

  if (method === "POST" && path === "/api/services") {
    const name = String(body.name ?? "").trim();
    if (!name) throw new ApiError(422, "Nome do serviço é obrigatório.");
    const sloId = body.slo_id ? String(body.slo_id) : null;
    const slo = sloId ? sloCatalog.find((s) => s.id === sloId) : undefined;
    const service: Service = {
      id: nextId("svc"),
      name,
      slo_id: sloId,
      slo_name: slo?.name ?? null,
      current_status: sloId ? "operational" : "not_configured",
      last_status_change_at: new Date().toISOString(),
    };
    services.push(service);
    return service;
  }

  // -- Domínios ----------------------------------------------------------------

  if (method === "GET" && path === "/api/domains") {
    return domains;
  }

  if (method === "POST" && path === "/api/domains") {
    const hostname = String(body.hostname ?? "").trim();
    if (!hostname) throw new ApiError(422, "Hostname é obrigatório.");
    if (domains.some((d) => d.hostname.toLowerCase() === hostname.toLowerCase())) {
      throw new ApiError(409, "Esse hostname já está cadastrado.");
    }
    const domain: Domain = { id: nextId("dom"), hostname, created_at: new Date().toISOString() };
    domains.push(domain);
    return domain;
  }

  // -- Status Pages --------------------------------------------------------------

  if (method === "GET" && path === "/api/status-pages") {
    return statusPages.map((p) => advanceStatusPagePolling(p));
  }

  if (method === "POST" && path === "/api/status-pages") {
    const name = String(body.name ?? "").trim();
    const subdomain = String(body.subdomain ?? "").trim();
    const domainId = String(body.domain_id ?? "");
    if (!name || !subdomain || !domainId) {
      throw new ApiError(422, "Nome, subdomínio e domínio são obrigatórios.");
    }
    const page: StatusPage = {
      id: nextId("sp"),
      name,
      subdomain,
      domain_id: domainId,
      state: "draft",
      tls_last_error: null,
      created_at: new Date().toISOString(),
      service_ids: Array.isArray(body.service_ids) ? (body.service_ids as string[]) : [],
    };
    statusPages.push(page);
    return page;
  }

  // -- Incidentes ----------------------------------------------------------------

  if (method === "GET" && path === "/api/incidents") {
    return incidents;
  }

  if (method === "POST" && path === "/api/incidents") {
    const title = String(body.title ?? "").trim();
    if (!title) throw new ApiError(422, "Título é obrigatório.");
    const incident: Incident = {
      id: nextId("inc"),
      title,
      status: "investigating",
      created_at: new Date().toISOString(),
      resolved_at: null,
      service_ids: Array.isArray(body.service_ids) ? (body.service_ids as string[]) : [],
    };
    incidents.unshift(incident);
    return incident;
  }

  const incidentUpdatesMatch = path.match(/^\/api\/incidents\/([^/]+)\/updates$/);
  if (incidentUpdatesMatch) {
    const incidentId = incidentUpdatesMatch[1];
    if (method === "GET") {
      return incidentUpdates
        .filter((u) => u.incident_id === incidentId)
        .sort((a, b) => b.created_at.localeCompare(a.created_at));
    }
    if (method === "POST") {
      const text = String(body.body ?? "").trim();
      if (!text) throw new ApiError(422, "Update não pode ser vazio.");
      const update: IncidentUpdate = {
        id: nextId("upd"),
        incident_id: incidentId,
        body: text,
        created_at: new Date().toISOString(),
      };
      incidentUpdates.push(update);
      return incidentUpdates
        .filter((u) => u.incident_id === incidentId)
        .sort((a, b) => b.created_at.localeCompare(a.created_at));
    }
  }

  const incidentMatch = path.match(/^\/api\/incidents\/([^/]+)$/);
  if (incidentMatch && method === "PATCH") {
    const incident = incidents.find((i) => i.id === incidentMatch[1]);
    if (!incident) throw new ApiError(404, "Incidente não encontrado.");
    const status = String(body.status ?? "");
    incident.status = status as Incident["status"];
    incident.resolved_at = status === "resolved" ? new Date().toISOString() : null;
    return incident;
  }

  // -- Admins ----------------------------------------------------------------------

  if (method === "GET" && path === "/api/admins") {
    return adminsWithPending();
  }

  if (method === "POST" && path === "/api/admins/invite") {
    requireOwner();
    const email = String(body.email ?? "").trim();
    const role = String(body.role ?? "viewer") as Role;
    if (!email) throw new ApiError(422, "E-mail é obrigatório.");
    if (
      admins.some((a) => a.email.toLowerCase() === email.toLowerCase()) ||
      adminInvites.some((i) => i.email.toLowerCase() === email.toLowerCase())
    ) {
      throw new ApiError(409, "Já existe um admin ou convite para esse e-mail.");
    }
    const invite = {
      id: nextId("invite"),
      email,
      role,
      status: "pending" as const,
      expires_at: new Date(Date.now() + 1000 * 60 * 60 * 24 * 3).toISOString(),
    };
    adminInvites.push(invite);
    return invite;
  }

  const inviteResendMatch = path.match(/^\/api\/admins\/invites\/([^/]+)\/resend$/);
  if (inviteResendMatch && method === "POST") {
    requireOwner();
    const invite = adminInvites.find((i) => i.id === inviteResendMatch[1]);
    if (!invite) throw new ApiError(404, "Convite não encontrado.");
    invite.expires_at = new Date(Date.now() + 1000 * 60 * 60 * 24 * 3).toISOString();
    return invite;
  }

  const inviteCancelMatch = path.match(/^\/api\/admins\/invites\/([^/]+)$/);
  if (inviteCancelMatch && method === "DELETE") {
    requireOwner();
    const idx = adminInvites.findIndex((i) => i.id === inviteCancelMatch[1]);
    if (idx === -1) throw new ApiError(404, "Convite não encontrado.");
    adminInvites.splice(idx, 1);
    return {};
  }

  const adminMatch = path.match(/^\/api\/admins\/([^/]+)$/);
  if (adminMatch && method === "PATCH") {
    requireOwner();
    const admin = admins.find((a) => a.id === adminMatch[1]);
    if (!admin) throw new ApiError(404, "Admin não encontrado.");
    const newRole = String(body.role ?? "") as Role;
    if (admin.role === "owner" && newRole !== "owner") {
      const remainingOwners = admins.filter((a) => a.role === "owner" && a.id !== admin.id);
      if (remainingOwners.length === 0) {
        throw new ApiError(409, "Não é possível remover o último owner.");
      }
    }
    admin.role = newRole;
    return toPublicAdmin(admin);
  }

  if (adminMatch && method === "DELETE") {
    requireOwner();
    const admin = admins.find((a) => a.id === adminMatch[1]);
    if (!admin) throw new ApiError(404, "Admin não encontrado.");
    if (admin.role === "owner") {
      const remainingOwners = admins.filter((a) => a.role === "owner" && a.id !== admin.id);
      if (remainingOwners.length === 0) {
        throw new ApiError(409, "Não é possível remover o último owner.");
      }
    }
    const idx = admins.findIndex((a) => a.id === admin.id);
    admins.splice(idx, 1);
    return {};
  }

  // -- Poller ------------------------------------------------------------------------

  if (method === "GET" && path === "/api/poller/status") {
    return pollerStatus;
  }

  throw new ApiError(404, `Rota mock não encontrada: ${method} ${path}`);
}

export async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  await delay();
  return (await handleRoute(path, init)) as T;
}
