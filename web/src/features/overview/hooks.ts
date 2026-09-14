import { useQuery } from "@tanstack/react-query";
import { apiFetch } from "../../lib/apiClient";
import type { OverviewResponse } from "../../types/api";

// useOverview fetches the tenant-wide aggregation summary from
// GET /api/overview (dashboard-overview-page). The endpoint returns a flat
// OverviewResponse, not a Page<T> envelope, so there is no page number in
// the queryKey.
export function useOverview() {
  return useQuery({
    queryKey: ["overview"],
    queryFn: () => apiFetch<OverviewResponse>("/api/overview"),
  });
}
