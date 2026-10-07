import { useMutation, useQuery, useQueryClient, type Query } from "@tanstack/react-query";
import { apiFetch } from "../../lib/apiClient";
import type { Page, StatusPage, StatusPageEnvironment, StatusPageServiceGroup, StatusPageState } from "../../types/api";

const terminalStates: StatusPageState[] = ["published", "tls_failed"];

function isTerminal(state: StatusPageState | undefined): boolean {
  return state === undefined || terminalStates.includes(state);
}

export function useStatusPages(page: number) {
  return useQuery({
    queryKey: ["status-pages", page],
    queryFn: () => apiFetch<Page<StatusPage>>(`/api/status-pages?page=${page}`),
    refetchInterval: (query: Query<Page<StatusPage>>) => {
      const data = query.state.data;
      if (!data) return false;
      const anyPending = data.items.some((p) => !isTerminal(p.state));
      return anyPending ? 10_000 : false;
    },
  });
}

/** Deriva uma única status page da mesma query/cache da página 1 da listagem,
 * com seu próprio `refetchInterval` — para na tela de detalhe (T27) assim que
 * ESSE item específico chega a um estado terminal, independentemente dos
 * demais. Só busca a página do item na página 1 (instalação single-tenant,
 * AD-002 — a status page recém-criada sempre está na primeira página). */
export function useStatusPage(id: string) {
  return useQuery({
    queryKey: ["status-pages", 1],
    queryFn: () => apiFetch<Page<StatusPage>>("/api/status-pages?page=1"),
    select: (data: Page<StatusPage>) => data.items.find((p) => p.id === id),
    refetchInterval: (query: Query<Page<StatusPage>>) => {
      const data = query.state.data;
      const page = data?.items.find((p) => p.id === id);
      return isTerminal(page?.state) ? false : 10_000;
    },
  });
}

// SPD-01: the SPA always creates a status page domain-less now - the
// backend still accepts the with-domain shape (design.md Tech Decisions),
// but this input never sends domain_id/subdomain. A domain is attached
// later, exclusively through useAttachDomain below.
export interface CreateStatusPageInput {
  name: string;
  service_ids: string[];
  // environment is optional (status-page-environment-banner SPEB-01/03):
  // omitted/"" means no banner (production implied) - the backend
  // defaults it the same way when the field is absent from the request
  // body entirely, so callers that don't care about it can keep omitting
  // it.
  environment?: StatusPageEnvironment;
}

export function useCreateStatusPage() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateStatusPageInput) =>
      apiFetch<StatusPage>("/api/status-pages", { method: "POST", body: JSON.stringify(input) }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["status-pages"] });
    },
  });
}

export interface AttachDomainInput {
  id: string;
  domain_id: string;
  subdomain: string;
}

// useAttachDomain sets domain_id/subdomain exactly once on a domain-less
// status page (SPD-06 through SPD-09) via PATCH /api/status-pages/{id}/domain.
// 404/409/422 responses surface as ApiError - the caller (AttachDomainDrawer)
// renders them inline instead of this hook interpreting them.
export function useAttachDomain() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, domain_id, subdomain }: AttachDomainInput) =>
      apiFetch<StatusPage>(`/api/status-pages/${id}/domain`, {
        method: "PATCH",
        body: JSON.stringify({ domain_id, subdomain }),
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["status-pages"] });
    },
  });
}

export interface UpdateStatusPageEnvironmentInput {
  id: string;
  environment: StatusPageEnvironment;
}

// useUpdateStatusPageEnvironment sets a status page's banner environment
// tag (status-page-environment-banner SPEB-02/03) via PATCH
// /api/status-pages/{id}/environment - a dedicated single-purpose
// mutation, same pattern as useAttachDomain above (this codebase has no
// generic "update status page" endpoint).
export function useUpdateStatusPageEnvironment() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, environment }: UpdateStatusPageEnvironmentInput) =>
      apiFetch<StatusPage>(`/api/status-pages/${id}/environment`, {
        method: "PATCH",
        body: JSON.stringify({ environment }),
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["status-pages"] });
    },
  });
}

// ServicePlacement mirrors internal/api/status_pages_handler.go's
// servicePlacementRequest (T6) - one service's placement within a status
// page's service list: which group it belongs to (if any), which service
// it's nested under as a dependency (if any), and its display position.
export interface ServicePlacement {
  service_id: string;
  group_id?: string;
  parent_service_id?: string;
  position: number;
}

export interface SetStatusPageServicesInput {
  id: string;
  services: ServicePlacement[];
}

// useSetStatusPageServices replaces the full set of services shown on a
// status page (SPD-15, SGD-02/SGD-03/SGD-06) via PATCH
// /api/status-pages/{id}/services. Unlike useCreateStatusPage's one-time
// service_ids, this can be called repeatedly as an admin edits which
// services a page shows (and how they're grouped/nested) after creation -
// each call replaces the full placement set rather than merging into it.
export function useSetStatusPageServices() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, services }: SetStatusPageServicesInput) =>
      apiFetch<StatusPage>(`/api/status-pages/${id}/services`, {
        method: "PATCH",
        body: JSON.stringify({ services }),
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["status-pages"] });
    },
  });
}

// useStatusPageGroups reads the named service groups on a status page
// (SGD-01) via GET /api/status-pages/{id}/groups, position-ordered.
export function useStatusPageGroups(statusPageId: string) {
  return useQuery({
    queryKey: ["status-pages", statusPageId, "groups"],
    queryFn: () =>
      apiFetch<StatusPageServiceGroup[]>(`/api/status-pages/${statusPageId}/groups`),
  });
}

export interface CreateGroupInput {
  statusPageId: string;
  name: string;
}

// useCreateGroup creates a new named, ordered service group scoped to a
// status page (SGD-01) via POST /api/status-pages/{id}/groups.
export function useCreateGroup() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ statusPageId, name }: CreateGroupInput) =>
      apiFetch<StatusPageServiceGroup>(`/api/status-pages/${statusPageId}/groups`, {
        method: "POST",
        body: JSON.stringify({ name }),
      }),
    onSuccess: (_data, variables) => {
      queryClient.invalidateQueries({ queryKey: ["status-pages", variables.statusPageId, "groups"] });
    },
  });
}

export interface UpdateGroupInput {
  statusPageId: string;
  groupId: string;
  name: string;
  position: number;
}

// useUpdateGroup renames and/or repositions an existing service group
// (SGD-05) via PATCH /api/status-pages/{id}/groups/{groupId}.
export function useUpdateGroup() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ statusPageId, groupId, name, position }: UpdateGroupInput) =>
      apiFetch<StatusPageServiceGroup>(`/api/status-pages/${statusPageId}/groups/${groupId}`, {
        method: "PATCH",
        body: JSON.stringify({ name, position }),
      }),
    onSuccess: (_data, variables) => {
      queryClient.invalidateQueries({ queryKey: ["status-pages", variables.statusPageId, "groups"] });
    },
  });
}

export interface DeleteGroupInput {
  statusPageId: string;
  groupId: string;
}

// useDeleteGroup removes a service group while leaving its member
// services in place, ungrouped (SGD-04) via DELETE
// /api/status-pages/{id}/groups/{groupId}. Also invalidates the
// service-placement query (not just the groups list) - the backend
// ungroups the deleted group's members server-side, so ServiceGroupPanel's
// working tree needs the refreshed group_id=null placements too, or it
// would keep rendering those services under a group id that no longer
// exists (M3 fix).
export function useDeleteGroup() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ statusPageId, groupId }: DeleteGroupInput) =>
      apiFetch<void>(`/api/status-pages/${statusPageId}/groups/${groupId}`, { method: "DELETE" }),
    onSuccess: (_data, variables) => {
      queryClient.invalidateQueries({ queryKey: ["status-pages", variables.statusPageId, "groups"] });
      queryClient.invalidateQueries({ queryKey: ["status-pages", variables.statusPageId, "service-placements"] });
    },
  });
}

// useStatusPageServicePlacements reads the full per-service placement
// detail (service_id, group_id, parent_service_id, position) for a status
// page (SGD-02, SGD-03, SGD-06, SGD-07) via GET
// /api/status-pages/{id}/services - distinct from useStatusPage(s)' plain
// service_ids: string[].
export function useStatusPageServicePlacements(statusPageId: string) {
  return useQuery({
    queryKey: ["status-pages", statusPageId, "service-placements"],
    queryFn: () => apiFetch<ServicePlacement[]>(`/api/status-pages/${statusPageId}/services`),
  });
}

export function useDeleteStatusPage() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => apiFetch<void>(`/api/status-pages/${id}`, { method: "DELETE" }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["status-pages"] });
    },
  });
}

export interface VerifyDomainResult {
  hostname: string;
  resolved_ips: string[];
  dns_resolved: boolean;
  // null when the operator never configured PUBLIC_DNS_TARGET - nothing
  // to compare against. Computed server-side by IP-set overlap, not by
  // comparing DNS record strings (a CNAME chain of any length, or a plain
  // A record with no CNAME at all, both defeat a literal string compare).
  dns_matches_target: boolean | null;
  tls_reachable: boolean;
  // true only if tls_reachable AND the served certificate chain verifies
  // against the system root pool for this hostname - tls_reachable alone
  // just means *something* answered TLS (a parked domain, a wrong cert,
  // a self-signed cert all complete a handshake too).
  tls_cert_valid: boolean;
  tls_error: string | null;
  state: StatusPageState;
  tls_last_error: string | null;
  checked_at: string;
}

// useVerifyDomain triggers POST /api/status-pages/{id}/verify-domain: a
// real DNS lookup + TLS handshake against the page's public hostname
// (mirrors the "recheck DNS/SSL" action platforms like Vercel/Render
// offer for custom domains). Invalidates the status-pages query on
// success since a successful handshake can transition state server-side
// (pending_tls -> published/tls_failed) even without waiting for the next
// 10s poll.
export function useVerifyDomain() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) =>
      apiFetch<VerifyDomainResult>(`/api/status-pages/${id}/verify-domain`, { method: "POST" }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["status-pages"] });
    },
  });
}

interface DNSTargetResponse {
  target: string | null;
}

// useDNSTarget reads the operator-configured DNS record value (SPD-10),
// or null when PUBLIC_DNS_TARGET was never set - the caller shows a note,
// never blocks submission on it (design.md Assumptions).
export function useDNSTarget() {
  return useQuery({
    queryKey: ["instance-dns-target"],
    queryFn: () => apiFetch<DNSTargetResponse>("/api/instance/dns-target"),
    select: (data: DNSTargetResponse) => data.target,
  });
}
