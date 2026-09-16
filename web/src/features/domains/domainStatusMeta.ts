import type { TagVariant } from "../../components/ui/Tag";
import type { Domain, DomainSSLStatus, DomainStatus } from "../../types/api";

// Shared status badge metadata for every DomainStatus/DomainSSLStatus value
// (domains-status-pages-page T5), mirroring services/statusMeta.ts's
// pattern so the redesigned Domínios tab's Status/SSL columns and detail
// drawer read from a single source of truth.
export const domainStatusLabel: Record<DomainStatus, string> = {
  pending: "Pendente",
  verified: "Verificado",
  error: "Erro",
};

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

export const sslStatusLabel: Record<DomainSSLStatus, string> = {
  pending: "Pendente",
  active: "Ativo",
  error: "Erro",
};

export const sslStatusColor: Record<DomainSSLStatus, string> = {
  pending: "var(--color-warning)",
  active: "var(--color-success)",
  error: "var(--color-critical)",
};

export const domainTypeLabel: Record<Domain["domain_type"], string> = {
  custom: "Domínio próprio",
};

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
