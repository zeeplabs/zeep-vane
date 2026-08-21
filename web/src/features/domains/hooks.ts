import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiFetch } from "../../lib/apiClient";
import type { Domain } from "../../types/api";

export function useDomains() {
  return useQuery({
    queryKey: ["domains"],
    queryFn: () => apiFetch<Domain[]>("/api/domains"),
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
