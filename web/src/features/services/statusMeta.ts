import type { TagVariant } from "../../components/ui/Tag";
import type { ServiceStatus } from "../../types/api";

// Shared status badge metadata for every ServiceStatus value, used by both
// ServicesSection (compact embed, IntegrationsPage) and ServiceListPage/
// ServiceDetailDrawer (monitored-services-page redesign) so the 4-state
// badge has a single source of truth (design.md's Tech Decision).
//
// SPEC_DEVIATION: statusLabel.outage was "Inoperante" in ServicesSection's
// original inline copy; spec.md SVC-04 and the design mock
// (handoff-new-layout/Servicos Monitorados.dc.html) both require "Inativo".
// Hoisting picks the spec-correct value rather than preserving the stale
// one, since no existing test asserts on the old "Inoperante" string.
export const statusLabel: Record<ServiceStatus, string> = {
  operational: "Operacional",
  degraded: "Degradado",
  outage: "Inativo",
  not_configured: "Não configurado",
};

export const statusVariant: Record<ServiceStatus, TagVariant> = {
  operational: "success",
  degraded: "warning",
  outage: "critical",
  not_configured: "neutral-outline",
};

export const statusDotColor: Record<ServiceStatus, string> = {
  operational: "var(--color-success)",
  degraded: "var(--color-warning)",
  outage: "var(--color-critical)",
  not_configured: "var(--color-neutral-600)",
};
