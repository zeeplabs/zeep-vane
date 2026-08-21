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

export interface CreateStatusPageInput {
  name: string;
  subdomain: string;
  domain_id: string;
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
