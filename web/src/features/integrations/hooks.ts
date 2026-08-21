import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiFetch, ApiError } from "../../lib/apiClient";
import type { SLOSummary } from "../../types/api";

// Adapted from the real backend's GET /api/integrations/datadog/status
// (SPEC_DEVIATION, I15): the backend never echoes a masked key (SP-01.4) -
// masked_key stays permanently undefined here, kept only so
// IntegrationsPage's optional-chaining fallback ("••••" placeholder)
// continues to render something instead of the real key.
export interface IntegrationStatusResponse {
  connected: boolean;
  status?: "active" | "invalid";
  last_checked_at?: string | null;
  last_error?: string | null;
  masked_key?: string;
}

interface DatadogStatusPayload {
  status: "active" | "invalid";
  last_checked_at: string | null;
  last_error: string | null;
}

export function useIntegrationStatus() {
  return useQuery({
    queryKey: ["integrations", "datadog"],
    queryFn: async (): Promise<IntegrationStatusResponse> => {
      try {
        const data = await apiFetch<DatadogStatusPayload>("/api/integrations/datadog/status");
        return { connected: true, ...data };
      } catch (err) {
        if (err instanceof ApiError && err.status === 404) {
          return { connected: false };
        }
        throw err;
      }
    },
  });
}

export interface ConnectDatadogInput {
  api_key: string;
  app_key: string;
}

// The real backend's POST /api/integrations/datadog only ever returns
// {"status":"connected"} (SP-01.4 - never a masked_key), unlike the
// mock-era response this type used to model.
interface ConnectDatadogResponse {
  status: "connected";
  masked_key?: string;
}

export function useConnectDatadog() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: ConnectDatadogInput) =>
      apiFetch<ConnectDatadogResponse>("/api/integrations/datadog", {
        method: "POST",
        body: JSON.stringify(input),
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["integrations", "datadog"] });
    },
  });
}

export function useSLOSearch(query: string) {
  return useQuery({
    queryKey: ["integrations", "datadog", "slos", query],
    queryFn: () => apiFetch<SLOSummary[]>(`/api/integrations/datadog/slos?query=${encodeURIComponent(query)}`),
    enabled: query.trim().length > 0,
  });
}

// fetchSLOName resolves a service's slo_id to its human-readable SLO name
// by reusing the search endpoint with an id: filter (I15) - the real
// backend's Service model only stores the opaque slo_id (design.md), it
// has no slo_name field, so services/hooks.ts calls this to fill the gap
// client-side rather than needing a new backend endpoint.
export async function fetchSLOName(sloId: string): Promise<string | null> {
  const results = await apiFetch<SLOSummary[]>(
    `/api/integrations/datadog/slos?query=${encodeURIComponent("id:" + sloId)}`
  );
  return results.find((slo) => slo.id === sloId)?.name ?? null;
}
