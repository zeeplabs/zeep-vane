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

export interface PublicHistoryBucket {
  start: string;
  status: PublicHourlyStatus;
}

// RangeKey is the set of selectable time-range tiers for the public status
// page's history/uptime window (public-status-time-range-selector,
// TRS-07) - mirrors the Go backend's rangeSpecs map keys exactly
// (internal/api/time_range.go).
export type RangeKey = "24h" | "7d" | "30d" | "90d";

export interface PublicServiceEntry {
  name: string;
  status: PublicServiceStatus;
  last_updated_at: string | null;
  history: PublicHistoryBucket[];
  // uptime_percent is null ("undefined", render a dash) when the service
  // has zero recorded intervals within the selected range's window
  // (backend: internal/api/public_status_handler.go's publicServiceResponse
  // doc comment) - never a fabricated 0 or 100. Recomputed for whichever
  // range is currently selected (TRS-04), not pinned to 24h.
  uptime_percent: number | null;
  // status_analysis is the LLM-generated degraded-tooltip text (AI-14,
  // AI-16), present only when status is "degraded" and an analysis has
  // finished generating - absent (not null, matches the backend's
  // `omitempty`) otherwise, including while a degraded service's analysis
  // is still pending.
  status_analysis?: string;
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
  // description is an optional longer body (AI-09/AI-11/AI-18) - absent for
  // a manually-created incident, or an auto-created one still on its
  // generic title-only text. pending_close_comment is deliberately never
  // exposed here - admin-only.
  description?: string;
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
  // resolvedTotal is the backend's total count of resolved incidents across
  // all pages (list-pagination T13) - incidents.resolved above only holds
  // what's been loaded so far (page 1, plus any page appended by
  // loadMoreResolvedIncidents). Compare its length against this to know
  // whether "Carregar mais" (T20) should still show.
  resolvedTotal: number;
}
