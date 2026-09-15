import type { IncidentSeverity, IncidentStatus } from "../../types/api";
import type { TagVariant } from "../../components/ui/Tag";

// Mirrors the mock's STATUS_META (handoff-new-layout/Incidentes.dc.html):
// investigating=critical(red), monitoring=warning(amber), resolved=success
// (green). "identified" has no mock equivalent (its seed data never uses
// that status) - mapped to accent (purple) to stay visually distinct
// without inventing a color the design system doesn't already have.
export const incidentStatusLabel: Record<IncidentStatus, string> = {
  investigating: "Investigando",
  identified: "Identificado",
  monitoring: "Monitorando",
  resolved: "Resolvido",
};

export const incidentStatusVariant: Record<IncidentStatus, TagVariant> = {
  investigating: "critical",
  identified: "accent",
  monitoring: "warning",
  resolved: "success",
};

export const incidentStatusDotColor: Record<IncidentStatus, string> = {
  investigating: "var(--color-critical)",
  identified: "var(--color-accent)",
  monitoring: "var(--color-warning)",
  resolved: "var(--color-success)",
};

// Mirrors the mock's SEVERITY_META label/color mapping.
export const incidentSeverityLabel: Record<IncidentSeverity, string> = {
  minor: "Menor",
  moderate: "Moderado",
  critical: "Crítico",
};

export const incidentSeverityVariant: Record<IncidentSeverity, TagVariant> = {
  minor: "neutral-outline",
  moderate: "warning",
  critical: "critical",
};

// Formats elapsed time between two ISO timestamps as "XhYmin"/"Ymin",
// matching the mock's "2h 10min"/"38min" duration column.
export function formatDuration(startIso: string, endIso: string | null): string {
  const start = new Date(startIso).getTime();
  const end = endIso ? new Date(endIso).getTime() : Date.now();
  const totalMinutes = Math.max(0, Math.round((end - start) / 60000));
  const hours = Math.floor(totalMinutes / 60);
  const minutes = totalMinutes % 60;
  if (hours === 0) return `${minutes}min`;
  return `${hours}h ${minutes}min`;
}
