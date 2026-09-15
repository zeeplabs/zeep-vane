import type { TagVariant } from "../../components/ui/Tag";
import type { DomainSSLStatus, DomainStatus } from "../../types/api";

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
