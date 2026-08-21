import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiFetch } from "../../lib/apiClient";
import type { SLOSummary } from "../../lib/mockData";

export interface IntegrationStatusResponse {
  connected: boolean;
  status?: "active" | "invalid";
  last_checked_at?: string | null;
  last_error?: string | null;
  masked_key?: string;
}

export function useIntegrationStatus() {
  return useQuery({
    queryKey: ["integrations", "datadog"],
    queryFn: () => apiFetch<IntegrationStatusResponse>("/api/integrations/datadog"),
  });
}

export interface ConnectDatadogInput {
  api_key: string;
  app_key: string;
}

export function useConnectDatadog() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: ConnectDatadogInput) =>
      apiFetch<IntegrationStatusResponse>("/api/integrations/datadog", {
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
