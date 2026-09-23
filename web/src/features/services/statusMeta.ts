import type { TagVariant } from "../../components/ui/Tag";
import type { Service, ServiceStatus } from "../../types/api";

type Translator = (key: string, options?: Record<string, unknown>) => string;

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
export function statusLabel(t: Translator, status: ServiceStatus): string {
  return t(`services.statusLabel.${status}`);
}

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

// serviceSubtitle is the shared row/drawer subtitle (ServiceListPage.tsx's
// table row and ServiceDetailDrawer.tsx's header) - manual-polling-
// monitoring T9: a polling-manual service has no slo_name/slo_id at all, so
// `slo_name || slo_id` alone renders blank/undefined for it. Checking
// monitor_mode explicitly (rather than falling through to poll_target only
// when both slo fields are falsy) matches the read path every other service
// already uses with zero special-casing beyond this one branch (spec.md's
// own "no special-casing in read paths" success criterion).
export function serviceSubtitle(service: Pick<Service, "monitor_mode" | "poll_target" | "slo_name" | "slo_id">): string {
  if (service.monitor_mode === "polling") {
    return service.poll_target ?? "";
  }
  return service.slo_name || service.slo_id || "";
}
