import { useQuery } from "@tanstack/react-query";
import { apiFetch } from "../../lib/apiClient";
import type { AuditLogEntry, OverviewResponse } from "../../types/api";

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

// useRecentActivity fetches the tenant's last 5 admin actions from
// GET /api/audit-log?limit=5, powering the "Atividade recente do time"
// card (recent-team-activity, ACTIVITY-09). Its own useQuery, independent
// of useOverview - design.md's "one card, one hook" idiom for this page.
export function useRecentActivity() {
  return useQuery({
    queryKey: ["audit-log"],
    queryFn: () => apiFetch<AuditLogEntry[]>("/api/audit-log?limit=5"),
  });
}
