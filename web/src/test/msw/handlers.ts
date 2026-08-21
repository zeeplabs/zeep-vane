import { http, HttpResponse } from "msw";
import {
  admins as seedAdmins,
  domains as seedDomains,
  statusPages as seedStatusPages,
  services as seedServices,
  incidents as seedIncidents,
  incidentUpdates as seedIncidentUpdates,
} from "../../lib/mockData";
import type { Domain, StatusPage } from "../../types/api";

// Simulates the vane_session cookie server-side: real cookie semantics
// (Set-Cookie/credentials) are covered by the Go integration tests
// (I1-I4); this in-memory flag only lets the auth handlers below model
// "logged in or not" across requests within a test.
let sessionAdminId: string | null = null;

export function resetAuthSession(): void {
  sessionAdminId = null;
}

// In-memory domains/status-pages state, seeded fresh from mockData's
// fixtures on every resetDomainsAndStatusPages() call (test/setup.ts
// afterEach) - mutated by the POST handlers below the same way a real DB
// row would be, scoped to a single test.
let domainsState: Domain[] = [];
let statusPagesState: StatusPage[] = [];
let domainIdCounter = 0;
let statusPageIdCounter = 0;

export function resetDomainsAndStatusPages(): void {
  domainsState = seedDomains.map((d) => ({ ...d }));
  statusPagesState = seedStatusPages.map((p) => ({ ...p }));
  domainIdCounter = 0;
  statusPageIdCounter = 0;
}
resetDomainsAndStatusPages();

// toDomainResponse/toStatusPageResponse strip fields the real backend
// never returns (StatusPage.service_ids only exists in the frontend
// fixture, for UI convenience - GET /api/status-pages, like
// POST /api/status-pages before it, never includes it).
function toDomainResponse(domain: Domain) {
  return { id: domain.id, hostname: domain.hostname, created_at: domain.created_at };
}

function toStatusPageResponse(statusPage: StatusPage) {
  return {
    id: statusPage.id,
    name: statusPage.name,
    subdomain: statusPage.subdomain,
    domain_id: statusPage.domain_id,
    state: statusPage.state,
    tls_last_error: statusPage.tls_last_error,
    created_at: statusPage.created_at,
  };
}

export const handlers = [
  http.post("/api/auth/login", async ({ request }) => {
    const body = (await request.json()) as { email?: string; password?: string };
    const admin = seedAdmins.find((a) => a.email === body.email && a.password === body.password);
    if (!admin) {
      return HttpResponse.json({ error: "invalid email or password" }, { status: 401 });
    }
    sessionAdminId = admin.id;
    return HttpResponse.json({ token: `msw-token-${admin.id}` });
  }),

  http.get("/api/auth/me", () => {
    const admin = seedAdmins.find((a) => a.id === sessionAdminId);
    if (!admin) {
      return HttpResponse.json({ error: "unauthorized" }, { status: 401 });
    }
    return HttpResponse.json({ id: admin.id, email: admin.email, role: admin.role });
  }),

  http.post("/api/auth/logout", () => {
    sessionAdminId = null;
    return new HttpResponse(null, { status: 200 });
  }),

  http.get("/api/domains", () => {
    if (!sessionAdminId) return HttpResponse.json({ error: "unauthorized" }, { status: 401 });
    return HttpResponse.json(domainsState.map(toDomainResponse));
  }),

  http.post("/api/domains", async ({ request }) => {
    if (!sessionAdminId) return HttpResponse.json({ error: "unauthorized" }, { status: 401 });
    const body = (await request.json()) as { hostname?: string };
    if (!body.hostname) {
      return HttpResponse.json({ error: "hostname is required" }, { status: 422 });
    }
    if (domainsState.some((d) => d.hostname === body.hostname)) {
      return HttpResponse.json({ error: "hostname already registered" }, { status: 409 });
    }
    domainIdCounter += 1;
    const created: Domain = {
      id: `dom-msw-${domainIdCounter}`,
      hostname: body.hostname,
      created_at: new Date().toISOString(),
    };
    domainsState.push(created);
    return HttpResponse.json(toDomainResponse(created), { status: 201 });
  }),

  http.get("/api/status-pages", () => {
    if (!sessionAdminId) return HttpResponse.json({ error: "unauthorized" }, { status: 401 });
    return HttpResponse.json(statusPagesState.map(toStatusPageResponse));
  }),

  http.post("/api/status-pages", async ({ request }) => {
    if (!sessionAdminId) return HttpResponse.json({ error: "unauthorized" }, { status: 401 });
    const body = (await request.json()) as {
      name?: string;
      subdomain?: string;
      domain_id?: string;
      service_ids?: string[];
    };
    if (!body.name || !body.subdomain || !body.domain_id) {
      return HttpResponse.json({ error: "name, subdomain, and domain_id are required" }, { status: 422 });
    }
    statusPageIdCounter += 1;
    const created: StatusPage = {
      id: `sp-msw-${statusPageIdCounter}`,
      name: body.name,
      subdomain: body.subdomain,
      domain_id: body.domain_id,
      state: "draft",
      tls_last_error: null,
      created_at: new Date().toISOString(),
      service_ids: body.service_ids ?? [],
    };
    statusPagesState.push(created);
    return HttpResponse.json(toStatusPageResponse(created), { status: 201 });
  }),

  // GET /api/status-pages/:id/public-preview (I12/I13) - mirrors the real
  // backend's PublicStatusHandler.composeResponse shape exactly
  // ({services,incidents}, no service_names on incidents - the backend
  // doesn't return that either, see public-status/hooks.ts SPEC_DEVIATION),
  // gated the same way router.HostRouter gates production traffic: only a
  // "published" status page composes, anything else 404s.
  http.get("/api/status-pages/:id/public-preview", ({ params }) => {
    if (!sessionAdminId) return HttpResponse.json({ error: "unauthorized" }, { status: 401 });
    const page = statusPagesState.find((p) => p.id === params.id);
    if (!page || page.state !== "published") {
      return new HttpResponse(null, { status: 404 });
    }

    const pageServices = seedServices.filter(
      (s) => page.service_ids.includes(s.id) && s.current_status !== "not_configured",
    );
    const relevantIncidents = seedIncidents.filter((inc) =>
      inc.service_ids.some((sid) => page.service_ids.includes(sid)),
    );

    const toPreviewIncident = (incident: (typeof seedIncidents)[number]) => ({
      id: incident.id,
      title: incident.title,
      status: incident.status,
      created_at: incident.created_at,
      resolved_at: incident.resolved_at,
      updates: seedIncidentUpdates
        .filter((u) => u.incident_id === incident.id)
        .map((u) => ({ body: u.body, created_at: u.created_at })),
    });

    const retentionCutoffMs = Date.now() - 1000 * 60 * 60 * 24 * 90;
    const active = relevantIncidents.filter((inc) => inc.status !== "resolved").map(toPreviewIncident);
    const resolved = relevantIncidents
      .filter(
        (inc) =>
          inc.status === "resolved" &&
          inc.resolved_at !== null &&
          new Date(inc.resolved_at).getTime() >= retentionCutoffMs,
      )
      .map(toPreviewIncident);

    return HttpResponse.json({
      services: pageServices.map((s) => ({
        name: s.name,
        status: s.current_status,
        last_updated_at: s.last_status_change_at,
      })),
      incidents: { active, resolved },
    });
  }),
];
