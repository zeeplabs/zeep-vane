import { useCallback, useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { apiFetch } from "../../lib/apiClient";
import type { PublicHourlyBucket, PublicIncidentEntry, PublicStatusPageData, PublicServiceStatus } from "../../lib/publicStatus";

interface PreviewCompany {
  name: string;
  logo_url: string | null;
}

interface PreviewIncidentUpdate {
  body: string;
  created_at: string;
}

interface PreviewIncident {
  id: string;
  title: string;
  status: PublicIncidentEntry["status"];
  created_at: string;
  resolved_at: string | null;
  updates: PreviewIncidentUpdate[];
}

interface PreviewService {
  name: string;
  status: PublicServiceStatus;
  last_updated_at: string;
  hourly_history: PublicHourlyBucket[];
}

interface PreviewResolvedPage {
  items: PreviewIncident[];
  total: number;
  page: number;
  page_size: number;
}

interface PreviewResponse {
  company: PreviewCompany;
  services: PreviewService[];
  incidents: { active: PreviewIncident[]; resolved: PreviewResolvedPage };
}

// SPEC_DEVIATION (AD-007, I13): the real backend's public response has no
// notion of which service names an incident is linked to (only
// incident_services IDs, never exposed publicly) - service_names stays
// empty until that gap closes. It never fabricates a value.
function toPublicIncidentEntry(incident: PreviewIncident): PublicIncidentEntry {
  return {
    id: incident.id,
    title: incident.title,
    status: incident.status,
    created_at: incident.created_at,
    resolved_at: incident.resolved_at,
    service_names: [],
    updates: incident.updates,
  };
}

// latestServiceChange returns the most recent last_updated_at among
// services, or null if none have one yet (never a fabricated "now" -
// mirrors the backend's own SP-08/SP-09 guarantee).
function latestServiceChange(services: PreviewService[]): string | null {
  return services.reduce<string | null>((latest, service) => {
    if (!service.last_updated_at) return latest;
    if (!latest) return service.last_updated_at;
    return new Date(service.last_updated_at).getTime() > new Date(latest).getTime() ? service.last_updated_at : latest;
  }, null);
}

async function fetchPublicStatusPage(id: string, resolvedPage: number): Promise<PublicStatusPageData> {
  const data = await apiFetch<PreviewResponse>(`/api/status-pages/${id}/public-preview?page=${resolvedPage}`);
  const latestChange = latestServiceChange(data.services);

  return {
    company_name: data.company.name,
    logo_url: data.company.logo_url,
    updated_at: latestChange ?? new Date().toISOString(),
    stale: false,
    services: data.services.map((service) => ({
      name: service.name,
      status: service.status,
      last_updated_at: service.last_updated_at,
      hourly_history: service.hourly_history,
    })),
    incidents: {
      active: data.incidents.active.map(toPublicIncidentEntry),
      resolved: data.incidents.resolved.items.map(toPublicIncidentEntry),
    },
    resolvedTotal: data.incidents.resolved.total,
  };
}

// usePublicStatusPage fetches page 1 of resolved incidents through the
// normal polled query (refetchInterval unchanged), then tracks any
// additional pages loaded via loadMoreResolvedIncidents in local state -
// appended after page 1's items, never replacing/reordering them
// (list-pagination T13). Extra pages reset whenever `id` changes so
// switching status pages never leaks the previous page's loaded history.
export function usePublicStatusPage(id: string) {
  const [extraResolved, setExtraResolved] = useState<PublicIncidentEntry[]>([]);
  const [loadedPage, setLoadedPage] = useState(1);

  useEffect(() => {
    setExtraResolved([]);
    setLoadedPage(1);
  }, [id]);

  const query = useQuery({
    queryKey: ["public-status-page", id],
    queryFn: () => fetchPublicStatusPage(id, 1),
    refetchInterval: 120_000,
    retry: false,
  });

  const loadMoreResolvedIncidents = useCallback(async () => {
    const nextPage = loadedPage + 1;
    const next = await fetchPublicStatusPage(id, nextPage);
    setExtraResolved((prev) => [...prev, ...next.incidents.resolved]);
    setLoadedPage(nextPage);
  }, [id, loadedPage]);

  const data: PublicStatusPageData | undefined = query.data && {
    ...query.data,
    incidents: {
      ...query.data.incidents,
      resolved: [...query.data.incidents.resolved, ...extraResolved],
    },
  };
  const resolvedTotal = query.data?.resolvedTotal ?? 0;
  const hasMoreResolved = (data?.incidents.resolved.length ?? 0) < resolvedTotal;

  return { ...query, data, resolvedTotal, hasMoreResolved, loadMoreResolvedIncidents };
}
