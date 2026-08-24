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

interface PreviewResponse {
  company: PreviewCompany;
  services: PreviewService[];
  incidents: { active: PreviewIncident[]; resolved: PreviewIncident[] };
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

export function usePublicStatusPage(id: string) {
  return useQuery({
    queryKey: ["public-status-page", id],
    queryFn: async (): Promise<PublicStatusPageData> => {
      const data = await apiFetch<PreviewResponse>(`/api/status-pages/${id}/public-preview`);
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
          resolved: data.incidents.resolved.map(toPublicIncidentEntry),
        },
      };
    },
    refetchInterval: 120_000,
    retry: false,
  });
}
