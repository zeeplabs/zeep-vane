// Shape of the public status page's data, described in
// status-page-handoff/README.md ("Data shape"), sem os campos exclusivos do
// admin (ids internos, tls, domain_id etc). The actual data now comes from
// features/public-status/hooks.ts's real backend call (I13) - this file
// only holds the shared shape both that hook and PublicStatusPage.tsx
// import.
export type PublicServiceStatus = "operational" | "degraded" | "outage";

// PublicHourlyStatus adds "no_data" on top of PublicServiceStatus: an
// hourly bucket the poller never recorded anything for (UPT-06), distinct
// from any real observed status.
export type PublicHourlyStatus = PublicServiceStatus | "no_data";

export interface PublicHourlyBucket {
  start: string;
  status: PublicHourlyStatus;
}

export interface PublicServiceEntry {
  name: string;
  status: PublicServiceStatus;
  last_updated_at: string | null;
  hourly_history: PublicHourlyBucket[];
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
