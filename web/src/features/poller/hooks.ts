import { useQuery } from "@tanstack/react-query";
import { apiFetch } from "../../lib/apiClient";
import type { PollerStatusResponse } from "../../types/api";

export function usePollerStatus(page: number) {
  return useQuery({
    queryKey: ["poller", "status", page],
    queryFn: () => apiFetch<PollerStatusResponse>(`/api/poller/status?page=${page}`),
    refetchInterval: 30_000,
  });
}
