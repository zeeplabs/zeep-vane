import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiFetch } from "../../lib/apiClient";
import type { Service, ServiceStatus } from "../../types/api";
import { fetchSLOName } from "../integrations/hooks";

// The real backend's serviceResponse (internal/api/services_handler.go) has
// no slo_name field - only the opaque slo_id. toService (SPEC_DEVIATION,
// I15) fills it in via fetchSLOName, reusing the SLO search endpoint.
interface ServiceResponse {
  id: string;
  name: string;
  slo_id: string;
  current_status: ServiceStatus;
  last_status_change_at: string;
}

async function toService(raw: ServiceResponse): Promise<Service> {
  return {
    id: raw.id,
    name: raw.name,
    slo_id: raw.slo_id,
    slo_name: await fetchSLOName(raw.slo_id),
    current_status: raw.current_status,
    last_status_change_at: raw.last_status_change_at,
  };
}

export function useServices() {
  return useQuery({
    queryKey: ["services"],
    queryFn: async (): Promise<Service[]> => {
      const raw = await apiFetch<ServiceResponse[]>("/api/services");
      return Promise.all(raw.map(toService));
    },
  });
}

export interface CreateServiceInput {
  name: string;
  slo_id: string;
}

export function useCreateService() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (input: CreateServiceInput): Promise<Service> => {
      const raw = await apiFetch<ServiceResponse>("/api/services", {
        method: "POST",
        body: JSON.stringify(input),
      });
      return toService(raw);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["services"] });
    },
  });
}
