import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiFetch } from "../../lib/apiClient";
import type { Domain, Page } from "../../types/api";

export function useDomains(page: number) {
  return useQuery({
    queryKey: ["domains", page],
    queryFn: () => apiFetch<Page<Domain>>(`/api/domains?page=${page}`),
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
