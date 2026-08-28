import { http, HttpResponse } from "msw";
import {
  admins as seedAdmins,
  adminInvites as seedAdminInvites,
  toPublicAdmin,
  domains as seedDomains,
  statusPages as seedStatusPages,
  services as seedServices,
  incidents as seedIncidents,
  incidentUpdates as seedIncidentUpdates,
  sloCatalog,
  datadogIntegration as seedDatadogIntegration,
  pollerStatus as seedPollerStatus,
  companySettings as seedCompanySettings,
} from "../../lib/mockData";
import type {
  Admin,
  AdminInvite,
  Role,
  Domain,
  StatusPage,
  Service,
  Incident,
  IncidentUpdate,
  IncidentStatus,
  CompanySettings,
  Page,
} from "../../types/api";

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

// GET /api/instance/dns-target's configured value (SPD-10) - mirrors
// config.PublicDNSTarget. Defaults to a configured value; tests that need
// the "operator never configured it" (null) case override via
// server.use(http.get("/api/instance/dns-target", ...)).
const dnsTargetState: string | null = "203.0.113.10";

// In-memory services + Datadog integration state (I15), seeded the same
// way as domains/status-pages above.
let servicesState: Service[] = [];
let serviceIdCounter = 0;
let datadogConnected = false;
let datadogStatus: "active" | "invalid" = "active";
let datadogLastError: string | null = null;

export function resetServicesAndIntegration(): void {
  servicesState = seedServices.map((s) => ({ ...s }));
  serviceIdCounter = 0;
  datadogConnected = seedDatadogIntegration.connected;
  datadogStatus = seedDatadogIntegration.status;
  datadogLastError = seedDatadogIntegration.last_error;
}
resetServicesAndIntegration();

// In-memory email provider state (EMAIL-01..06) - no seed fixture, since
// spec.md's default is "no provider has ever been connected" (empty list,
// active_provider null); tests that need a connected/active provider drive
// it through the connect/activate handlers below, same as a real flow
// would.
interface EmailProviderRecord {
  provider: "sendgrid" | "resend";
  status: "connected" | "invalid";
  from_email: string;
  from_name: string;
  last_checked_at: string | null;
  last_error: string | null;
}

let emailProvidersState: EmailProviderRecord[] = [];
let emailActiveProvider: "sendgrid" | "resend" | null = null;

export function resetEmailProviders(): void {
  emailProvidersState = [];
  emailActiveProvider = null;
}
resetEmailProviders();

// In-memory incidents + timeline state (I16), seeded the same way as
// domains/status-pages/services above. incident_updates is real-backend
// shaped: mirrors IncidentRepository.ListUpdates/AddUpdate - most recent
// first, one row per Transition too (internal/db/incident_repository.go).
let incidentsState: Incident[] = [];
let incidentUpdatesState: IncidentUpdate[] = [];
let incidentIdCounter = 0;
let incidentUpdateIdCounter = 0;

export function resetIncidents(): void {
  incidentsState = seedIncidents.map((i) => ({ ...i }));
  incidentUpdatesState = seedIncidentUpdates.map((u) => ({ ...u }));
  incidentIdCounter = 0;
  incidentUpdateIdCounter = 0;
}
resetIncidents();

// In-memory admins + pending invites state (I19), seeded the same way as
// domains/status-pages/services/incidents above. Mirrors AdminsHandler.List
// (internal/api/admins.go): active admins tagged status "active", pending
// invites (used_at is null, expires_at in the future - AdminInviteRepository
// .List) tagged "pending".
let adminsState: Admin[] = [];
let adminInvitesState: AdminInvite[] = [];
let adminInviteIdCounter = 0;

// adminInviteTokensState maps a raw accept-invite token to the invite it was
// issued for. The real backend never exposes a raw token to any client that
// didn't just mint it (only the emailed link carries one), so this mock-only
// map exists purely so a frontend test can drive
// POST /api/admins/invite/:token/accept with a token it chose itself - see
// seedAdminInviteToken.
let adminInviteTokensState: Record<string, { email: string; role: Role }> = {};

export function resetAdmins(): void {
  adminsState = seedAdmins.map((a) => toPublicAdmin(a));
  adminInvitesState = seedAdminInvites.map((i) => ({ ...i }));
  adminInviteIdCounter = 0;
  adminInviteTokensState = {};
}
resetAdmins();

// Test-only helper (accept-invite-page): registers rawToken as accepting for
// email/role via POST /api/admins/invite/:token/accept, mirroring how the
// real backend hashes a freshly-minted token at Invite() time.
export function seedAdminInviteToken(rawToken: string, email: string, role: Role): void {
  adminInviteTokensState[rawToken] = { email, role };
}

// Test-only helper (INVITE-07): seeds a pending invite whose expires_at is
// already in the past, so a test can assert List's expired:true tagging
// without waiting an hour or faking the clock.
export function seedExpiredAdminInvite(email: string, role: Role): void {
  adminInviteIdCounter += 1;
  adminInvitesState.push({
    id: `invite-msw-expired-${adminInviteIdCounter}`,
    email,
    role,
    status: "pending",
    expires_at: new Date(Date.now() - 1000 * 60 * 60).toISOString(),
  });
}

// In-memory company_settings state (SET-01, SET-07), seeded the same way
// as the other fixtures above - the real backend's row is a singleton, so
// this mirrors that with a single mutable object rather than an array.
let companySettingsState: CompanySettings = { ...seedCompanySettings };

export function resetCompanySettings(): void {
  companySettingsState = { ...seedCompanySettings };
}
resetCompanySettings();

// bootstrapState mirrors the real admins-table-empty check
// (BootstrapHandler.Status/Create, SHD-15/SHD-19): every fixture/test admin
// already exists, so this defaults to "bootstrapped" and stays that way
// unless a test overrides it via server.use() (same convention as
// dnsTargetState) - keeps every pre-existing AuthProvider-rendering test's
// needsBootstrap at its safe default (false) without having to touch them.
let bootstrapState = true;

export function resetBootstrapState(): void {
  bootstrapState = true;
}

// setBootstrapped lets a test set up the admin-less precondition (e.g. for
// BootstrapPage's "successful create" case) without a dedicated per-test
// server.use() override of both /api/bootstrap/status and /api/bootstrap.
export function setBootstrapped(value: boolean): void {
  bootstrapState = value;
}

const validAdminRoles: Role[] = ["owner", "operator", "viewer"];

// wouldLeaveZeroOwners mirrors admins.go's function of the same name
// (ADM-06 lockout decision).
function wouldLeaveZeroOwners(currentRole: Role, keepsOwnerRole: boolean, ownerCount: number): boolean {
  return currentRole === "owner" && !keepsOwnerRole && ownerCount <= 1;
}

function timelineFor(incidentId: string): IncidentUpdate[] {
  return incidentUpdatesState
    .filter((u) => u.incident_id === incidentId)
    .sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime());
}

// paginatedPage mirrors the backend's internal/api.Page[T] envelope
// (LIMIT/OFFSET slicing over an already-ordered array), reading ?page=
// from requestUrl the same way parsePage does: missing/invalid/non-positive
// clamps to 1.
function paginatedPage<T>(requestUrl: string, items: T[], pageSize: number): Page<T> {
  const rawPage = new URL(requestUrl).searchParams.get("page");
  const parsed = rawPage ? Number.parseInt(rawPage, 10) : 1;
  const page = Number.isFinite(parsed) && parsed >= 1 ? parsed : 1;
  const start = (page - 1) * pageSize;
  return {
    items: items.slice(start, start + pageSize),
    total: items.length,
    page,
    page_size: pageSize,
  };
}

// toServiceResponse strips slo_name - the real serviceResponse
// (internal/api/services_handler.go) never returns it, only the opaque
// slo_id (see services/hooks.ts's toService adapter, SPEC_DEVIATION I15).
function toServiceResponse(service: Service) {
  return {
    id: service.id,
    name: service.name,
    slo_id: service.slo_id,
    current_status: service.current_status,
    last_status_change_at: service.last_status_change_at,
  };
}

// toDomainResponse strips fields the real backend never returns.
// toStatusPageResponse mirrors the real StatusPagesHandler response shape,
// service_ids included - GET/POST/PATCH .../services on the real backend
// all return it (internal/api/status_pages_handler.go's
// toStatusPageResponse).
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
    service_ids: statusPage.service_ids,
  };
}

// buildFixtureHourlyHistory fabricates a plausible 24-bucket hourly_history
// for the public-preview MSW fixture only (never used by the real backend,
// which computes this from real status_snapshots -
// internal/history.BuildHourly). Every bucket mirrors the service's
// current status, giving PublicStatusPage.test.tsx real fixture data to
// render 24 same-colored bars against.
function buildFixtureHourlyHistory(status: Service["current_status"]) {
  const now = Date.now();
  return Array.from({ length: 24 }, (_, i) => ({
    start: new Date(now - (23 - i) * 60 * 60 * 1000).toISOString(),
    status: status === "not_configured" ? "no_data" : status,
  }));
}

export const handlers = [
  // GET /api/bootstrap/status - mirrors BootstrapHandler.Status: reports
  // whether any admin exists yet, for the SPA's boot-time redirect decision
  // (SHD-19). Public, unauthenticated - never gated on sessionAdminId.
  http.get("/api/bootstrap/status", () => {
    return HttpResponse.json({ bootstrapped: bootstrapState });
  }),

  // POST /api/bootstrap - mirrors BootstrapHandler.Create: creates the
  // first admin (owner role) when none exists yet, responding with the
  // same identity shape /api/auth/me returns (SHD-16, SHD-18); 409
  // "already bootstrapped" once one exists (SHD-15), 422 on an empty
  // email/password (SHD-14's own input-validation edge case). Deliberately
  // does not push into seedAdmins/adminsState - BootstrapPage's own tests
  // never depend on a subsequent /api/auth/me call succeeding within the
  // same test (that boot re-check only happens on a real page reload).
  http.post("/api/bootstrap", async ({ request }) => {
    const body = (await request.json()) as { email?: string; password?: string };
    if (!body.email || !body.password) {
      return HttpResponse.json({ error: "email and password are required" }, { status: 422 });
    }
    if (bootstrapState) {
      return HttpResponse.json({ error: "already bootstrapped" }, { status: 409 });
    }
    bootstrapState = true;
    return HttpResponse.json({ id: "admin-bootstrap-1", email: body.email, role: "owner" });
  }),

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

  // POST /api/status-pages (SPD-01, SPD-05) - mirrors the relaxed
  // StatusPagesHandler.Create: name is the only required field now: no
  // domain_id/subdomain at all creates a domain-less page (domain_id/
  // subdomain both null); giving exactly one of the pair is rejected,
  // since a domain without a subdomain (or vice versa) is meaningless.
  http.post("/api/status-pages", async ({ request }) => {
    if (!sessionAdminId) return HttpResponse.json({ error: "unauthorized" }, { status: 401 });
    const body = (await request.json()) as {
      name?: string;
      subdomain?: string;
      domain_id?: string;
      service_ids?: string[];
    };
    if (!body.name) {
      return HttpResponse.json({ error: "name is required" }, { status: 422 });
    }
    if (Boolean(body.subdomain) !== Boolean(body.domain_id)) {
      return HttpResponse.json(
        { error: "subdomain and domain_id must be set together, or not at all" },
        { status: 422 },
      );
    }
    statusPageIdCounter += 1;
    const created: StatusPage = {
      id: `sp-msw-${statusPageIdCounter}`,
      name: body.name,
      subdomain: body.subdomain ?? null,
      domain_id: body.domain_id ?? null,
      state: "draft",
      tls_last_error: null,
      created_at: new Date().toISOString(),
      service_ids: body.service_ids ?? [],
    };
    statusPagesState.push(created);
    return HttpResponse.json(toStatusPageResponse(created), { status: 201 });
  }),

  // PATCH /api/status-pages/:id/domain (SPD-06 through SPD-09) - mirrors
  // StatusPagesHandler.AttachDomain: 404 unknown page, 422 empty
  // subdomain/domain_id, 409 already attached, 422 domain_id that doesn't
  // reference a real Domain, 409 (domain_id, subdomain) pair already used
  // by another page, else 200 with the updated page.
  http.patch("/api/status-pages/:id/domain", async ({ request, params }) => {
    if (!sessionAdminId) return HttpResponse.json({ error: "unauthorized" }, { status: 401 });
    const page = statusPagesState.find((p) => p.id === params.id);
    if (!page) {
      return HttpResponse.json({ error: "status page not found" }, { status: 404 });
    }
    const body = (await request.json()) as { domain_id?: string; subdomain?: string };
    if (!body.domain_id || !body.subdomain) {
      return HttpResponse.json({ error: "domain_id and subdomain are required" }, { status: 422 });
    }
    if (page.domain_id) {
      return HttpResponse.json({ error: "this status page already has a domain attached" }, { status: 409 });
    }
    if (!domainsState.some((d) => d.id === body.domain_id)) {
      return HttpResponse.json({ error: "domain_id does not reference an existing domain" }, { status: 422 });
    }
    if (
      statusPagesState.some(
        (p) => p.id !== page.id && p.domain_id === body.domain_id && p.subdomain === body.subdomain,
      )
    ) {
      return HttpResponse.json({ error: "this domain/subdomain pair is already in use" }, { status: 409 });
    }
    page.domain_id = body.domain_id;
    page.subdomain = body.subdomain;
    return HttpResponse.json(toStatusPageResponse(page));
  }),

  // PATCH /api/status-pages/:id/services (SPD-15) - mirrors
  // StatusPagesHandler.SetServices: 404 unknown page, 422 missing
  // service_ids field (an empty array is valid - unlinks everything),
  // else 200 with the page's full linked set replaced.
  http.patch("/api/status-pages/:id/services", async ({ request, params }) => {
    if (!sessionAdminId) return HttpResponse.json({ error: "unauthorized" }, { status: 401 });
    const page = statusPagesState.find((p) => p.id === params.id);
    if (!page) {
      return HttpResponse.json({ error: "status page not found" }, { status: 404 });
    }
    const body = (await request.json()) as { service_ids?: string[] };
    if (!body.service_ids) {
      return HttpResponse.json({ error: "service_ids is required" }, { status: 422 });
    }
    page.service_ids = body.service_ids;
    return HttpResponse.json(toStatusPageResponse(page));
  }),

  // GET /api/instance/dns-target (SPD-10) - mirrors InstanceConfigHandler
  // .DNSTarget: the configured value, or null when the operator never set
  // PUBLIC_DNS_TARGET.
  http.get("/api/instance/dns-target", () => {
    if (!sessionAdminId) return HttpResponse.json({ error: "unauthorized" }, { status: 401 });
    return HttpResponse.json({ target: dnsTargetState });
  }),

  // GET /api/instance/branding - mirrors InstanceConfigHandler.Branding:
  // public/unauthenticated (login screen has no session yet), unlike
  // /api/company-settings.
  http.get("/api/instance/branding", () => {
    return HttpResponse.json({ logo_url: companySettingsState.logo_url });
  }),

  http.get("/api/integrations/datadog/status", () => {
    if (!sessionAdminId) return HttpResponse.json({ error: "unauthorized" }, { status: 401 });
    if (!datadogConnected) {
      return HttpResponse.json({ error: "datadog integration not connected yet" }, { status: 404 });
    }
    return HttpResponse.json({
      status: datadogStatus,
      last_checked_at: new Date().toISOString(),
      last_error: datadogLastError,
    });
  }),

  http.post("/api/integrations/datadog", async ({ request }) => {
    if (!sessionAdminId) return HttpResponse.json({ error: "unauthorized" }, { status: 401 });
    const body = (await request.json()) as { api_key?: string; app_key?: string };
    if (!body.api_key || !body.app_key) {
      return HttpResponse.json(
        { error: "invalid datadog api key or app key, or missing slo read permission" },
        { status: 422 },
      );
    }
    datadogConnected = true;
    datadogStatus = "active";
    datadogLastError = null;
    return HttpResponse.json({ status: "connected" }, { status: 201 });
  }),

  // GET /api/integrations/datadog/slos?query= (I14/I15) - mirrors
  // SearchSLOs: "id:<id>" does an exact id lookup (how services/hooks.ts's
  // fetchSLOName resolves slo_name), anything else is a case-insensitive
  // name substring match.
  http.get("/api/integrations/datadog/slos", ({ request }) => {
    if (!sessionAdminId) return HttpResponse.json({ error: "unauthorized" }, { status: 401 });
    const query = new URL(request.url).searchParams.get("query") ?? "";
    if (query.startsWith("id:")) {
      const id = query.slice(3);
      return HttpResponse.json(sloCatalog.filter((slo) => slo.id === id));
    }
    const needle = query.toLowerCase();
    return HttpResponse.json(sloCatalog.filter((slo) => slo.name.toLowerCase().includes(needle)));
  }),

  // GET /api/integrations/email (EMAIL-06) - mirrors
  // EmailProvidersHandler.List: never a 404, empty list + null
  // active_provider when nothing has ever been connected.
  http.get("/api/integrations/email", () => {
    if (!sessionAdminId) return HttpResponse.json({ error: "unauthorized" }, { status: 401 });
    return HttpResponse.json({
      active_provider: emailActiveProvider,
      providers: emailProvidersState,
    });
  }),

  // POST /api/integrations/email/:provider (EMAIL-01) - mirrors
  // EmailProvidersHandler.Connect: unknown provider 404, missing/invalid
  // input 422 (never persisted), success upserts and never echoes api_key.
  http.post("/api/integrations/email/:provider", async ({ request, params }) => {
    if (!sessionAdminId) return HttpResponse.json({ error: "unauthorized" }, { status: 401 });
    const provider = params.provider as string;
    if (provider !== "sendgrid" && provider !== "resend") {
      return HttpResponse.json({ error: "unknown email provider" }, { status: 404 });
    }
    const body = (await request.json()) as { api_key?: string; from_email?: string; from_name?: string };
    // "invalid-key" is this fixture's stand-in for a provider rejecting the
    // submitted key at validation time (EMAIL-01 AC1/AC2) - lets a test
    // drive the inline-422 path without a dedicated server.use() override.
    if (!body.api_key || !body.from_email || !body.from_name || body.api_key === "invalid-key") {
      return HttpResponse.json(
        { error: "invalid email provider api key, from_email, or from_name" },
        { status: 422 },
      );
    }
    const record: EmailProviderRecord = {
      provider,
      status: "connected",
      from_email: body.from_email,
      from_name: body.from_name,
      last_checked_at: new Date().toISOString(),
      last_error: null,
    };
    const existing = emailProvidersState.find((p) => p.provider === provider);
    if (existing) {
      Object.assign(existing, record);
    } else {
      emailProvidersState.push(record);
    }
    return HttpResponse.json({ status: "connected" }, { status: 201 });
  }),

  // POST /api/integrations/email/:provider/activate (EMAIL-04) - mirrors
  // EmailProvidersHandler.Activate: unknown provider 404, not-connected 422,
  // success flips active_provider.
  http.post("/api/integrations/email/:provider/activate", ({ params }) => {
    if (!sessionAdminId) return HttpResponse.json({ error: "unauthorized" }, { status: 401 });
    const provider = params.provider as string;
    if (provider !== "sendgrid" && provider !== "resend") {
      return HttpResponse.json({ error: "unknown email provider" }, { status: 404 });
    }
    const connected = emailProvidersState.find((p) => p.provider === provider && p.status === "connected");
    if (!connected) {
      return HttpResponse.json({ error: "email provider not connected" }, { status: 422 });
    }
    emailActiveProvider = provider;
    return HttpResponse.json({ status: "active" });
  }),

  http.get("/api/services", () => {
    if (!sessionAdminId) return HttpResponse.json({ error: "unauthorized" }, { status: 401 });
    return HttpResponse.json(servicesState.map(toServiceResponse));
  }),

  http.post("/api/services", async ({ request }) => {
    if (!sessionAdminId) return HttpResponse.json({ error: "unauthorized" }, { status: 401 });
    const body = (await request.json()) as { name?: string; slo_id?: string };
    if (!body.name || !body.slo_id) {
      return HttpResponse.json({ error: "name and slo_id are required" }, { status: 422 });
    }
    serviceIdCounter += 1;
    const created: Service = {
      id: `svc-msw-${serviceIdCounter}`,
      name: body.name,
      slo_id: body.slo_id,
      slo_name: sloCatalog.find((slo) => slo.id === body.slo_id)?.name ?? null,
      current_status: "not_configured",
      last_status_change_at: new Date().toISOString(),
    };
    servicesState.push(created);
    return HttpResponse.json(toServiceResponse(created), { status: 201 });
  }),

  // GET /api/incidents (I16) - mirrors IncidentsHandler.List: most recently
  // created first, each with its service_ids.
  http.get("/api/incidents", ({ request }) => {
    if (!sessionAdminId) return HttpResponse.json({ error: "unauthorized" }, { status: 401 });
    const sorted = [...incidentsState].sort(
      (a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime(),
    );
    return HttpResponse.json(paginatedPage(request.url, sorted, 25));
  }),

  http.post("/api/incidents", async ({ request }) => {
    if (!sessionAdminId) return HttpResponse.json({ error: "unauthorized" }, { status: 401 });
    const body = (await request.json()) as { title?: string; service_ids?: string[] };
    if (!body.title || !body.service_ids || body.service_ids.length === 0) {
      return HttpResponse.json({ error: "title and at least one service_id are required" }, { status: 422 });
    }
    incidentIdCounter += 1;
    const created: Incident = {
      id: `inc-msw-${incidentIdCounter}`,
      title: body.title,
      status: "investigating",
      created_at: new Date().toISOString(),
      resolved_at: null,
      service_ids: body.service_ids,
    };
    incidentsState.push(created);
    return HttpResponse.json(created, { status: 201 });
  }),

  // GET/POST /api/incidents/:id/updates (I16) - mirrors
  // IncidentsHandler.ListUpdates/AddUpdate: timeline ordered most-recent
  // first, 404 for an incident id that doesn't exist.
  http.get("/api/incidents/:id/updates", ({ request, params }) => {
    if (!sessionAdminId) return HttpResponse.json({ error: "unauthorized" }, { status: 401 });
    const incidentId = params.id as string;
    if (!incidentsState.some((i) => i.id === incidentId)) {
      return HttpResponse.json({ error: "incident not found" }, { status: 404 });
    }
    return HttpResponse.json(paginatedPage(request.url, timelineFor(incidentId), 25));
  }),

  http.post("/api/incidents/:id/updates", async ({ request, params }) => {
    if (!sessionAdminId) return HttpResponse.json({ error: "unauthorized" }, { status: 401 });
    const incidentId = params.id as string;
    if (!incidentsState.some((i) => i.id === incidentId)) {
      return HttpResponse.json({ error: "incident not found" }, { status: 404 });
    }
    const body = (await request.json()) as { body?: string };
    if (!body.body) {
      return HttpResponse.json({ error: "body is required" }, { status: 422 });
    }
    incidentUpdateIdCounter += 1;
    incidentUpdatesState.push({
      id: `upd-msw-${incidentUpdateIdCounter}`,
      incident_id: incidentId,
      body: body.body,
      created_at: new Date().toISOString(),
    });
    return HttpResponse.json(timelineFor(incidentId), { status: 201 });
  }),

  // PATCH /api/incidents/:id (I16) - mirrors IncidentsHandler.Transition:
  // sets resolved_at entering "resolved", clears it otherwise (reopening a
  // resolved incident is a legitimate transition, SP-20), records the
  // transition on the timeline.
  http.patch("/api/incidents/:id", async ({ request, params }) => {
    if (!sessionAdminId) return HttpResponse.json({ error: "unauthorized" }, { status: 401 });
    const incidentId = params.id as string;
    const incident = incidentsState.find((i) => i.id === incidentId);
    if (!incident) {
      return HttpResponse.json({ error: "incident not found" }, { status: 404 });
    }
    const body = (await request.json()) as { status?: IncidentStatus };
    const validStatuses: IncidentStatus[] = ["investigating", "identified", "monitoring", "resolved"];
    if (!body.status || !validStatuses.includes(body.status)) {
      return HttpResponse.json(
        { error: "status must be one of investigating, identified, monitoring, resolved" },
        { status: 422 },
      );
    }
    incident.status = body.status;
    incident.resolved_at = body.status === "resolved" ? new Date().toISOString() : null;
    incidentUpdateIdCounter += 1;
    incidentUpdatesState.push({
      id: `upd-msw-${incidentUpdateIdCounter}`,
      incident_id: incidentId,
      body: `Status changed to ${body.status}`,
      created_at: new Date().toISOString(),
    });
    return HttpResponse.json(incident);
  }),

  // GET /api/admins (I18/I19/INVITE-07) - mirrors AdminsHandler.List: active
  // admins (status "active") merged with every pending invite regardless of
  // expiry, each tagged expired:true once its expires_at has passed.
  http.get("/api/admins", () => {
    if (!sessionAdminId) return HttpResponse.json({ error: "unauthorized" }, { status: 401 });
    const active = adminsState.map((a) => ({ ...a, status: "active" as const }));
    const pending = adminInvitesState.map((i) => ({
      id: i.id,
      email: i.email,
      role: i.role,
      status: "pending" as const,
      expires_at: i.expires_at,
      expired: new Date(i.expires_at).getTime() <= Date.now(),
    }));
    return HttpResponse.json([...active, ...pending]);
  }),

  // POST /api/admins (I19/INVITE-01) - mirrors AdminsHandler.Invite: 422 on
  // missing email/invalid role, 409 if an active admin already owns the
  // email, otherwise replaces any pending invite for the email and issues a
  // new one. email_sent is always true here - there's no real email
  // provider in this mock environment to fail.
  http.post("/api/admins", async ({ request }) => {
    if (!sessionAdminId) return HttpResponse.json({ error: "unauthorized" }, { status: 401 });
    const body = (await request.json()) as { email?: string; role?: string };
    if (!body.email || !body.role || !validAdminRoles.includes(body.role as Role)) {
      return HttpResponse.json(
        { error: "email is required and role must be one of owner, operator, viewer" },
        { status: 422 },
      );
    }
    if (adminsState.some((a) => a.email === body.email)) {
      return HttpResponse.json({ error: "an active admin already exists for this email" }, { status: 409 });
    }
    adminInvitesState = adminInvitesState.filter((i) => i.email !== body.email);
    adminInviteIdCounter += 1;
    adminInvitesState.push({
      id: `invite-msw-${adminInviteIdCounter}`,
      email: body.email,
      role: body.role as Role,
      status: "pending",
      expires_at: new Date(Date.now() + 1000 * 60 * 60).toISOString(),
    });
    return HttpResponse.json({ status: "invited", email_sent: true }, { status: 201 });
  }),

  // POST /api/admins/invites/:id/resend (INVITE-03/04) - mirrors
  // AdminsHandler.ResendInvite: 404 unknown id, otherwise extends
  // expires_at by another hour (also un-expiring it, same as the real
  // Refresh) and reports email_sent:true.
  http.post("/api/admins/invites/:id/resend", ({ params }) => {
    if (!sessionAdminId) return HttpResponse.json({ error: "unauthorized" }, { status: 401 });
    const invite = adminInvitesState.find((i) => i.id === params.id);
    if (!invite) return HttpResponse.json({ error: "invite not found" }, { status: 404 });
    invite.expires_at = new Date(Date.now() + 1000 * 60 * 60).toISOString();
    return HttpResponse.json({ status: "resent", email_sent: true });
  }),

  // DELETE /api/admins/invites/:id (INVITE-05/06) - mirrors
  // AdminsHandler.CancelInvite: 404 unknown id, otherwise removes the
  // invite (the real backend sets used_at instead of deleting the row, but
  // from List's point of view - the only thing this mock exposes - the
  // outcome is identical: the invite stops appearing).
  http.delete("/api/admins/invites/:id", ({ params }) => {
    if (!sessionAdminId) return HttpResponse.json({ error: "unauthorized" }, { status: 401 });
    const invite = adminInvitesState.find((i) => i.id === params.id);
    if (!invite) return HttpResponse.json({ error: "invite not found" }, { status: 404 });
    adminInvitesState = adminInvitesState.filter((i) => i.id !== invite.id);
    return HttpResponse.json({ status: "canceled" });
  }),

  // POST /api/admins/invite/:token/accept (AIP-01/05/07/08/09) - mirrors
  // AdminsHandler.AcceptInvite: 422 on missing/weak password, 401 on an
  // unknown/already-consumed token, otherwise consumes the token (a second
  // accept with the same token 401s, matching the real single-use
  // invariant) and responds with the invite's email/role. Public - no
  // sessionAdminId gate, matching the real route's un-authenticated wiring.
  http.post("/api/admins/invite/:token/accept", async ({ request, params }) => {
    const body = (await request.json()) as { password?: string };
    if (!body.password) {
      return HttpResponse.json({ error: "password is required" }, { status: 422 });
    }
    if (body.password.length < 8 || body.password.length > 72) {
      return HttpResponse.json(
        { error: "password must be between 8 and 72 characters" },
        { status: 422 },
      );
    }
    const token = params.token as string;
    const entry = adminInviteTokensState[token];
    if (!entry) {
      return HttpResponse.json({ error: "invalid or expired invite token" }, { status: 401 });
    }
    delete adminInviteTokensState[token];
    return HttpResponse.json({ email: entry.email, role: entry.role }, { status: 201 });
  }),

  // PATCH /api/admins/:id/role (I19) - mirrors AdminsHandler.UpdateRole:
  // 404 unknown admin, 422 invalid role, 409 if this change would leave
  // zero active owners (ADM-06), otherwise applies the new role.
  http.patch("/api/admins/:id/role", async ({ request, params }) => {
    if (!sessionAdminId) return HttpResponse.json({ error: "unauthorized" }, { status: 401 });
    const admin = adminsState.find((a) => a.id === params.id);
    if (!admin) return HttpResponse.json({ error: "admin not found" }, { status: 404 });
    const body = (await request.json()) as { role?: string };
    if (!body.role || !validAdminRoles.includes(body.role as Role)) {
      return HttpResponse.json({ error: "role must be one of owner, operator, viewer" }, { status: 422 });
    }
    const ownerCount = adminsState.filter((a) => a.role === "owner").length;
    if (wouldLeaveZeroOwners(admin.role, body.role === "owner", ownerCount)) {
      return HttpResponse.json({ error: "this action would leave zero active owners" }, { status: 409 });
    }
    admin.role = body.role as Role;
    return HttpResponse.json({ id: admin.id, role: admin.role });
  }),

  // DELETE /api/admins/:id (I19) - mirrors AdminsHandler.Delete: 404 unknown
  // admin, 409 if removal would leave zero active owners (ADM-06),
  // otherwise removes the admin.
  http.delete("/api/admins/:id", ({ params }) => {
    if (!sessionAdminId) return HttpResponse.json({ error: "unauthorized" }, { status: 401 });
    const admin = adminsState.find((a) => a.id === params.id);
    if (!admin) return HttpResponse.json({ error: "admin not found" }, { status: 404 });
    const ownerCount = adminsState.filter((a) => a.role === "owner").length;
    if (wouldLeaveZeroOwners(admin.role, false, ownerCount)) {
      return HttpResponse.json({ error: "this action would leave zero active owners" }, { status: 409 });
    }
    adminsState = adminsState.filter((a) => a.id !== admin.id);
    return HttpResponse.json({ status: "removed" });
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
      company: { name: companySettingsState.name, logo_url: companySettingsState.logo_url },
      services: pageServices.map((s) => ({
        name: s.name,
        status: s.current_status,
        last_updated_at: s.last_status_change_at,
        hourly_history: buildFixtureHourlyHistory(s.current_status),
      })),
      incidents: { active, resolved },
    });
  }),

  // GET /api/poller/status (I20) - mirrors PollerStatusHandler.List
  // (internal/api/poller_status.go): read-only reflection of the last
  // status the poller persisted per integration, no live re-check.
  // Read-only, no per-test state to reset - always seeded from mockData.
  http.get("/api/poller/status", () => {
    if (!sessionAdminId) return HttpResponse.json({ error: "unauthorized" }, { status: 401 });
    return HttpResponse.json(seedPollerStatus);
  }),

  // GET/PATCH /api/company-settings, POST /api/company-settings/logo
  // (SET-01, SET-07) - mirrors CompanySettingsHandler: PATCH persists only
  // {name, contact_email} (logo goes through the separate multipart
  // upload below), the logo upload updates logo_url in state and returns
  // the full settings row, same as the real handler's shared
  // toCompanySettingsResponse.
  http.get("/api/company-settings", () => {
    if (!sessionAdminId) return HttpResponse.json({ error: "unauthorized" }, { status: 401 });
    return HttpResponse.json(companySettingsState);
  }),

  http.patch("/api/company-settings", async ({ request }) => {
    if (!sessionAdminId) return HttpResponse.json({ error: "unauthorized" }, { status: 401 });
    const body = (await request.json()) as { name?: string; contact_email?: string };
    if (!body.name || !body.contact_email) {
      return HttpResponse.json(
        { error: "name is required and contact_email must be a valid e-mail address" },
        { status: 422 },
      );
    }
    companySettingsState = { ...companySettingsState, name: body.name, contact_email: body.contact_email };
    return HttpResponse.json(companySettingsState);
  }),

  http.post("/api/company-settings/logo", ({ request }) => {
    if (!sessionAdminId) return HttpResponse.json({ error: "unauthorized" }, { status: 401 });
    // Deliberately never reads the multipart body itself (no
    // request.formData()/arrayBuffer() call): jsdom's Blob/File stream
    // implementation does not interoperate with Node's fetch body reader
    // used by msw's node interceptor, and awaiting it here hangs the test
    // indefinitely - a jsdom-environment limitation, not a real-world one.
    // MIME/size validation is real backend logic covered by the Go
    // integration tests (T6); this fixture only needs to prove the upload
    // was a multipart POST and that logo_url updates in state on success.
    const contentType = request.headers.get("content-type") ?? "";
    if (!contentType.startsWith("multipart/form-data")) {
      return HttpResponse.json({ error: "logo must be a PNG or SVG image no larger than 10 MB" }, { status: 422 });
    }
    companySettingsState = { ...companySettingsState, logo_url: "/uploads/logo" };
    return HttpResponse.json(companySettingsState);
  }),
];
