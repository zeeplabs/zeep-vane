import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiFetch } from "../../lib/apiClient";
import type { MonitorMode, Page, PollType, Service, ServiceDetail, ServiceStatus } from "../../types/api";

// The real backend's serviceResponse (internal/api/services_handler.go) now
// returns slo_name/uptime_30d/last_seen_at directly - toService is a plain,
// synchronous field mapping, no live Datadog call (design.md removes the
// old per-row fetchSLOName lookup entirely). monitor_mode/poll_type/
// poll_target/poll_interval_seconds (manual-polling-monitoring T9) mirror
// the backend's own additive response fields.
interface ServiceResponse {
  id: string;
  name: string;
  slo_id: string;
  slo_name: string;
  monitor_mode: MonitorMode;
  poll_type: PollType | null;
  poll_target: string | null;
  poll_interval_seconds: number | null;
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
    monitor_mode: raw.monitor_mode,
    poll_type: raw.poll_type,
    poll_target: raw.poll_target,
    poll_interval_seconds: raw.poll_interval_seconds,
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

// CreateServiceInput mirrors POST /api/services's extended request contract
// (design.md's CreateServiceRequest): monitor_mode is optional and omitted
// entirely for the slo-mode path (existing behavior, unchanged request
// shape); the poll_* fields only apply when monitor_mode is "polling".
// AddServiceDrawer sends exactly one mode's fields per submission, never
// both (T8's own "Done when" contract).
export interface CreateServiceInput {
  name: string;
  monitor_mode?: MonitorMode;
  slo_id?: string;
  slo_name?: string;
  // slo_type/datadog_service_tag ride along with slo_id/slo_name, captured
  // from the same SLO-search result (slo-root-cause-enrichment RCA-01) -
  // AddServiceDrawer.tsx omits both when the selected SLO has no single
  // resolved service tag (a flow-type SLO).
  slo_type?: string;
  datadog_service_tag?: string;
  poll_type?: PollType;
  poll_target?: string;
  poll_interval_seconds?: 30 | 60 | 300;
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

// useUpdateService renames a service via PATCH /api/services/{id}
// (service-edit SVCEDIT-01..05). Invalidates both the list and the detail
// query for id so the drawer and the list row pick up the new name without
// a full page reload (SVCEDIT-06).
export function useUpdateService() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ id, name }: { id: string; name: string }): Promise<Service> => {
      const raw = await apiFetch<ServiceResponse>(`/api/services/${id}`, {
        method: "PATCH",
        body: JSON.stringify({ name }),
      });
      return toService(raw);
    },
    onSuccess: (_data, variables) => {
      queryClient.invalidateQueries({ queryKey: ["services"] });
      queryClient.invalidateQueries({ queryKey: ["services", "detail", variables.id] });
    },
  });
}

// useDeleteService soft-deletes a service via DELETE /api/services/{id}
// (service-delete SVCDEL-01..05, 09). Invalidates the list so the row
// disappears without a full page reload. A 409 (still attached to a
// status page) or 404 propagates as an ApiError for the caller to surface.
export function useDeleteService() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => apiFetch<void>(`/api/services/${id}`, { method: "DELETE" }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["services"] });
    },
  });
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
