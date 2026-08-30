import { useQuery } from "@tanstack/react-query";
import { apiFetch } from "../../lib/apiClient";
import type { Page, PollerStatusEntry } from "../../types/api";

export function usePollerStatus(page: number) {
  return useQuery({
    queryKey: ["poller", "status", page],
    queryFn: () => apiFetch<Page<PollerStatusEntry>>(`/api/poller/status?page=${page}`),
    refetchInterval: 30_000,
  });
}
