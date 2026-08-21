import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiFetch } from "../../lib/apiClient";
import type { Service } from "../../types/api";

export function useServices() {
  return useQuery({
    queryKey: ["services"],
    queryFn: () => apiFetch<Service[]>("/api/services"),
  });
}

export interface CreateServiceInput {
  name: string;
  slo_id?: string | null;
}

export function useCreateService() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateServiceInput) =>
      apiFetch<Service>("/api/services", { method: "POST", body: JSON.stringify(input) }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["services"] });
    },
  });
}
