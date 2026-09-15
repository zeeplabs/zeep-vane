import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiFetch } from "../../lib/apiClient";
import type { Domain } from "../../types/api";

// DomainsPageResponse mirrors DomainsHandler.List's real envelope
// (internal/api/domains_handler.go's domainsPageResponse) - a bespoke shape,
// not the generic Page<T>, since dns_target sits loose alongside the list
// fields (AGENTS.md §4's convention for endpoints with non-list fields).
// useDomains previously typed this as Page<Domain>, silently dropping
// dns_target (design.md's flagged type drift) - fixed here even though this
// feature itself doesn't consume dns_target, so the next caller that needs
// it isn't starting from a wrong type.
export interface DomainsPageResponse {
  items: Domain[];
  total: number;
  page: number;
  page_size: number;
  dns_target: string | null;
}

export function useDomains(page: number) {
  return useQuery({
    queryKey: ["domains", page],
    queryFn: () => apiFetch<DomainsPageResponse>(`/api/domains?page=${page}`),
  });
}

export interface CreateDomainInput {
  hostname: string;
}

export function useCreateDomain() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateDomainInput) =>
      apiFetch<Domain>("/api/domains", { method: "POST", body: JSON.stringify(input) }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["domains"] });
    },
  });
}

export function useDeleteDomain() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => apiFetch<void>(`/api/domains/${id}`, { method: "DELETE" }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["domains"] });
    },
  });
}
