import { useQuery } from "@tanstack/react-query";
import { apiFetch } from "../../lib/apiClient";
import type { PollerStatusEntry } from "../../lib/mockData";

export function usePollerStatus() {
  return useQuery({
    queryKey: ["poller", "status"],
    queryFn: () => apiFetch<PollerStatusEntry[]>("/api/poller/status"),
    refetchInterval: 30_000,
  });
}
