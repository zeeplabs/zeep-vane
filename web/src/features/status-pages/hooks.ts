import { useMutation, useQuery, useQueryClient, type Query } from "@tanstack/react-query";
import { apiFetch } from "../../lib/apiClient";
import type { StatusPage, StatusPageState } from "../../types/api";

const terminalStates: StatusPageState[] = ["published", "tls_failed"];

function isTerminal(state: StatusPageState | undefined): boolean {
  return state === undefined || terminalStates.includes(state);
}

export function useStatusPages() {
  return useQuery({
    queryKey: ["status-pages"],
    queryFn: () => apiFetch<StatusPage[]>("/api/status-pages"),
    refetchInterval: (query: Query<StatusPage[]>) => {
      const pages = query.state.data;
      if (!pages) return false;
      const anyPending = pages.some((p) => !isTerminal(p.state));
      return anyPending ? 10_000 : false;
    },
  });
}

/** Deriva uma única status page da mesma query/cache da listagem, com seu
 * próprio `refetchInterval` — para na tela de detalhe (T27) assim que ESSE
 * item específico chega a um estado terminal, independentemente dos demais. */
export function useStatusPage(id: string) {
  return useQuery({
    queryKey: ["status-pages"],
    queryFn: () => apiFetch<StatusPage[]>("/api/status-pages"),
    select: (pages: StatusPage[]) => pages.find((p) => p.id === id),
    refetchInterval: (query: Query<StatusPage[]>) => {
      const pages = query.state.data;
      const page = pages?.find((p) => p.id === id);
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
