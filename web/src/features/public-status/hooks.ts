import { useQuery } from "@tanstack/react-query";
import { apiFetch } from "../../lib/apiClient";
import type { PublicStatusPageData } from "../../lib/publicStatus";

export function usePublicStatusPage(id: string) {
  return useQuery({
    queryKey: ["public-status-page", id],
    queryFn: () => apiFetch<PublicStatusPageData>(`/api/public/status-pages/${id}`),
    refetchInterval: 120_000,
    retry: false,
  });
}
