import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiFetch } from "../../lib/apiClient";
import type { Page, Service, ServiceDetail, ServiceStatus } from "../../types/api";

// The real backend's serviceResponse (internal/api/services_handler.go) now
// returns slo_name/uptime_30d/last_seen_at directly - toService is a plain,
// synchronous field mapping, no live Datadog call (design.md removes the
// old per-row fetchSLOName lookup entirely).
interface ServiceResponse {
  id: string;
  name: string;
  slo_id: string;
  slo_name: string;
  current_status: ServiceStatus;
  last_status_change_at: string;
  uptime_30d: number | null;
  last_seen_at: string | null;
}

function toService(raw: ServiceResponse): Service {
  return {
    id: raw.id,
    name: raw.name,
    slo_id: raw.slo_id,
    slo_name: raw.slo_name,
    current_status: raw.current_status,
    last_status_change_at: raw.last_status_change_at,
    uptime_30d: raw.uptime_30d,
    last_seen_at: raw.last_seen_at,
  };
}

export function useServices(page: number) {
  return useQuery({
    queryKey: ["services", page],
    queryFn: async (): Promise<Page<Service>> => {
      const raw = await apiFetch<Page<ServiceResponse>>(`/api/services?page=${page}`);
      return { ...raw, items: raw.items.map(toService) };
    },
  });
}

export interface CreateServiceInput {
  name: string;
  slo_id: string;
  slo_name: string;
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

interface ServiceDetailResponse extends ServiceResponse {
  status_analysis: string | null;
  incidents_30d: number;
  hourly_buckets: ServiceDetail["hourly_buckets"];
}

function toServiceDetail(raw: ServiceDetailResponse): ServiceDetail {
  return {
    ...toService(raw),
    status_analysis: raw.status_analysis,
    incidents_30d: raw.incidents_30d,
    hourly_buckets: raw.hourly_buckets,
  };
}

// useServiceDetail fetches the monitored-services-page detail drawer's data
// (SVC-14..19). `id` is nullable so the drawer's "no service selected" state
// can call this hook unconditionally without triggering a request.
export function useServiceDetail(id: string | null) {
  return useQuery({
    queryKey: ["services", "detail", id],
    queryFn: async (): Promise<ServiceDetail> => {
      const raw = await apiFetch<ServiceDetailResponse>(`/api/services/${id}`);
      return toServiceDetail(raw);
    },
    enabled: id !== null,
  });
}
