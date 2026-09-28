import type { TagVariant } from "../../components/ui/Tag";
import type { Domain, DomainSSLStatus, DomainStatus } from "../../types/api";

type Translator = (key: string, options?: Record<string, unknown>) => string;

// Shared status badge metadata for every DomainStatus/DomainSSLStatus value
// (domains-status-pages-page T5), mirroring services/statusMeta.ts's
// pattern so the redesigned Domínios tab's Status/SSL columns and detail
// drawer read from a single source of truth.
export function domainStatusLabel(t: Translator, status: DomainStatus): string {
  return t(`domains.statusLabel.${status}`);
}

export const domainStatusVariant: Record<DomainStatus, TagVariant> = {
  pending: "warning",
  verified: "success",
  error: "critical",
};

export const domainStatusDotColor: Record<DomainStatus, string> = {
  pending: "var(--color-warning)",
  verified: "var(--color-success)",
  error: "var(--color-critical)",
};

export function sslStatusLabel(t: Translator, status: DomainSSLStatus): string {
  return t(`domains.sslStatusLabel.${status}`);
}

export const sslStatusColor: Record<DomainSSLStatus, string> = {
  pending: "var(--color-warning)",
  active: "var(--color-success)",
  error: "var(--color-critical)",
};

export function domainTypeLabel(t: Translator, type: Domain["domain_type"]): string {
  return t(`domains.typeLabel.${type}`);
}

// attachedPageColumn renders spec.md DSP-02/03/04's "Aponta para" value:
// "—" when nothing is attached, the (single) attached page's name, or that
// name plus " +N" when more than one page is attached. Shared by
// DomainsTable's column and DomainDetailDrawer's subtitle line so both
// read from the same logic.
export function attachedPageColumn(domain: Pick<Domain, "attached_page_name" | "attached_page_count">): string {
  if (!domain.attached_page_name || domain.attached_page_count === 0) return "—";
  const extra = domain.attached_page_count - 1;
  return extra > 0 ? `${domain.attached_page_name} +${extra}` : domain.attached_page_name;
}

// daysRemainingFromExpiry returns whole days from now until expiresAt, or
// null when there is no expiry to show (domain-health-monitoring DHM-04).
export function daysRemainingFromExpiry(expiresAt: string | null | undefined, now: Date = new Date()): number | null {
  if (!expiresAt) return null;
  return Math.floor((new Date(expiresAt).getTime() - now.getTime()) / (1000 * 60 * 60 * 24));
}

// expirationColor maps days remaining to its badge color: green farther than
// 30 days, yellow 15-30 days, red under 15 days (including already expired).
export function expirationColor(daysRemaining: number): string {
  if (daysRemaining < 15) return "var(--color-critical)";
  if (daysRemaining <= 30) return "var(--color-warning)";
  return "var(--color-success)";
}

// isDomainAtRisk reports whether a domain warrants an at-a-glance warning in
// the list: NS drift detected, or expiration within the 30-day window
// (domain-health-monitoring DHM-08).
export function isDomainAtRisk(domain: Pick<Domain, "ns_drift_detected" | "expires_at">, now: Date = new Date()): boolean {
  return domainRisk(domain, now) !== null;
}

// domainRisk classifies a domain's at-a-glance list warning (DHM-08): NS
// drift takes precedence over an approaching expiration, and a healthy or
// not-yet-checked domain is null.
export function domainRisk(domain: Pick<Domain, "ns_drift_detected" | "expires_at">, now: Date = new Date()): "drift" | "expiring" | null {
  if (domain.ns_drift_detected) return "drift";
  const days = daysRemainingFromExpiry(domain.expires_at, now);
  return days !== null && days <= 30 ? "expiring" : null;
}
