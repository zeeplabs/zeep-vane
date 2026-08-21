// Monta a resposta da status page pública a partir das fixtures do mock — mesmo
// formato descrito em status-page-handoff/README.md ("Data shape"), sem os
// campos exclusivos do admin (ids internos, tls, domain_id etc).
import { ApiError } from "./apiClient";
import { statusPages, services, incidents, incidentUpdates, companySettings } from "./mockData";

const incidentRetentionDays = 90;

export type PublicServiceStatus = "operational" | "degraded" | "outage";

export interface PublicServiceEntry {
  name: string;
  status: PublicServiceStatus;
  last_updated_at: string | null;
}

export interface PublicIncidentUpdateEntry {
  body: string;
  created_at: string;
}

export interface PublicIncidentEntry {
  id: string;
  title: string;
  status: "investigating" | "identified" | "monitoring" | "resolved";
  created_at: string;
  resolved_at: string | null;
  service_names: string[];
  updates: PublicIncidentUpdateEntry[];
}

export interface PublicStatusPageData {
  company_name: string;
  logo_url: string | null;
  updated_at: string;
  stale: boolean;
  services: PublicServiceEntry[];
  incidents: {
    active: PublicIncidentEntry[];
    resolved: PublicIncidentEntry[];
  };
}

export function getPublicStatusPageData(id: string): PublicStatusPageData {
  const page = statusPages.find((p) => p.id === id);
  if (!page || page.state !== "published") {
    throw new ApiError(404, "Status page não encontrada.");
  }

  const pageServices = services.filter(
    (s) => page.service_ids.includes(s.id) && s.current_status !== "not_configured",
  );
  const serviceNameById = new Map(services.map((s) => [s.id, s.name]));

  const relevantIncidents = incidents.filter((inc) =>
    inc.service_ids.some((sid) => page.service_ids.includes(sid)),
  );

  const toPublicIncident = (inc: (typeof incidents)[number]): PublicIncidentEntry => ({
    id: inc.id,
    title: inc.title,
    status: inc.status,
    created_at: inc.created_at,
    resolved_at: inc.resolved_at,
    service_names: inc.service_ids.map((sid) => serviceNameById.get(sid) ?? sid),
    updates: incidentUpdates
      .filter((u) => u.incident_id === inc.id)
      .sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime())
      .map((u) => ({ body: u.body, created_at: u.created_at })),
  });

  const now = Date.now();
  const retentionCutoff = now - 1000 * 60 * 60 * 24 * incidentRetentionDays;

  const active = relevantIncidents.filter((inc) => inc.status !== "resolved").map(toPublicIncident);
  const resolved = relevantIncidents
    .filter((inc) => inc.status === "resolved" && inc.resolved_at && new Date(inc.resolved_at).getTime() >= retentionCutoff)
    .map(toPublicIncident);

  const latestChange = pageServices.reduce<string | null>((acc, s) => {
    if (!acc) return s.last_status_change_at;
    return new Date(s.last_status_change_at).getTime() > new Date(acc).getTime() ? s.last_status_change_at : acc;
  }, null);

  return {
    company_name: companySettings.name,
    logo_url: companySettings.logo_url,
    updated_at: latestChange ?? new Date(now).toISOString(),
    stale: false,
    services: pageServices.map((s) => ({
      name: s.name,
      status: s.current_status as PublicServiceStatus,
      last_updated_at: s.last_status_change_at,
    })),
    incidents: { active, resolved },
  };
}
