import { useMutation, useQuery, useQueryClient, type Query } from "@tanstack/react-query";
import { apiFetch } from "../../lib/apiClient";
import type { Page, StatusPage, StatusPageState } from "../../types/api";

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

export interface SetStatusPageServicesInput {
  id: string;
  service_ids: string[];
}

// useSetStatusPageServices replaces the full set of services shown on a
// status page (SPD-15) via PATCH /api/status-pages/{id}/services. Unlike
// useCreateStatusPage's one-time service_ids, this can be called
// repeatedly as an admin edits which services a page shows after
// creation - each call replaces the set rather than merging into it.
export function useSetStatusPageServices() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, service_ids }: SetStatusPageServicesInput) =>
      apiFetch<StatusPage>(`/api/status-pages/${id}/services`, {
        method: "PATCH",
        body: JSON.stringify({ service_ids }),
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["status-pages"] });
    },
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
  resolved_cname: string | null;
  dns_resolved: boolean;
  tls_reachable: boolean;
  tls_dial_error: string | null;
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
