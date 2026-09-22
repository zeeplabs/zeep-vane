import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { MdChevronRight } from "react-icons/md";
import { Card } from "../../components/ui/Card";
import { Pager } from "../../components/ui/Pager";
import { Skeleton } from "../../components/ui/Skeleton";
import { EmptyState } from "../../layout/EmptyState";
import { formatDateTime } from "../../lib/formatDate";
import type { Domain } from "../../types/api";
import { useDomains } from "./hooks";
import { attachedPageColumn, domainTypeLabel, sslStatusColor, sslStatusLabel } from "./domainStatusMeta";
import { DomainStatusTag } from "./DomainStatusTag";

export interface DomainsTableProps {
  /** Chamado quando uma linha da tabela é selecionada (abre o
   * DomainDetailDrawer, T7). */
  onSelect: (domain: Domain) => void;
}

/** Tabela redesenhada da aba Domínios (spec.md DSP-01): Status/Domínio/
 * Tipo/Aponta para/SSL/Verificado, mesma estrutura de grid/linha/Pager que
 * ServiceListPage já estabeleceu. */
export function DomainsTable({ onSelect }: DomainsTableProps) {
  const { t, i18n } = useTranslation();
  const [page, setPage] = useState(1);
  const { data: domainsPage, isLoading } = useDomains(page);
  const domains = useMemo(() => domainsPage?.items ?? [], [domainsPage]);
  const totalPages = Math.max(1, Math.ceil((domainsPage?.total ?? 0) / (domainsPage?.page_size ?? 20)));

  if (isLoading) {
    return (
      <Card elevation="none" aria-busy="true" className="overflow-hidden border border-divider">
        <span className="sr-only">{t("domains.loading")}</span>
        <div className="grid grid-cols-[110px_1fr_140px_1fr_90px_140px_20px] items-center gap-3 border-b border-divider bg-card-header-bg px-5 py-2.5">
          <span className="text-[11px] font-bold uppercase tracking-wide text-neutral-400">Status</span>
          <span className="text-[11px] font-bold uppercase tracking-wide text-neutral-400">Domínio</span>
          <span className="text-[11px] font-bold uppercase tracking-wide text-neutral-400">Tipo</span>
          <span className="text-[11px] font-bold uppercase tracking-wide text-neutral-400">Aponta para</span>
          <span className="text-[11px] font-bold uppercase tracking-wide text-neutral-400">SSL</span>
          <span className="text-[11px] font-bold uppercase tracking-wide text-neutral-400">Verificado</span>
          <span />
        </div>
        {Array.from({ length: 5 }).map((_, i) => (
          <div
            key={i}
            className="grid grid-cols-[110px_1fr_140px_1fr_90px_140px_20px] items-center gap-3 border-b border-divider px-5 py-3.5 last:border-b-0"
          >
            <Skeleton width={70} height={20} radius={999} />
            <Skeleton width={160} height={14} />
            <Skeleton width={90} height={14} />
            <Skeleton width={110} height={14} />
            <Skeleton width={50} height={14} />
            <Skeleton width={80} height={14} />
            <Skeleton width={16} height={16} />
          </div>
        ))}
      </Card>
    );
  }

  if (domains.length === 0) {
    return <EmptyState title="Nenhum domínio cadastrado" description="Adicione um domínio para começar." />;
  }

  return (
    <div className="flex flex-col gap-3">
      <Card elevation="none" className="overflow-hidden border border-divider">
        <div className="grid grid-cols-[110px_1fr_140px_1fr_90px_140px_20px] items-center gap-3 border-b border-divider bg-card-header-bg px-5 py-2.5">
          <span className="text-[11px] font-bold uppercase tracking-wide text-neutral-400">Status</span>
          <span className="text-[11px] font-bold uppercase tracking-wide text-neutral-400">Domínio</span>
          <span className="text-[11px] font-bold uppercase tracking-wide text-neutral-400">Tipo</span>
          <span className="text-[11px] font-bold uppercase tracking-wide text-neutral-400">Aponta para</span>
          <span className="text-[11px] font-bold uppercase tracking-wide text-neutral-400">SSL</span>
          <span className="text-[11px] font-bold uppercase tracking-wide text-neutral-400">Verificado</span>
          <span />
        </div>
        {domains.map((domain) => (
          <div
            key={domain.id}
            data-testid="domain-row"
            onClick={() => onSelect(domain)}
            className="grid cursor-pointer grid-cols-[110px_1fr_140px_1fr_90px_140px_20px] items-center gap-3 border-b border-divider px-5 py-3.5 last:border-b-0 hover:bg-card-header-bg"
          >
            <DomainStatusTag status={domain.status} />
            <div className="min-w-0 truncate font-mono text-[13px] text-text">{domain.hostname}</div>
            <div className="text-[13px] font-medium text-text-muted">{domainTypeLabel[domain.domain_type]}</div>
            <div className="min-w-0 truncate text-[13px] font-medium text-text-muted">{attachedPageColumn(domain)}</div>
            <div className="text-[13px] font-semibold" style={{ color: sslStatusColor[domain.ssl_status] }}>
              {sslStatusLabel[domain.ssl_status]}
            </div>
            <div className="text-xs text-text-muted">
              {domain.verified_at ? formatDateTime(domain.verified_at, i18n.language) : "—"}
            </div>
            <MdChevronRight size={16} className="text-neutral-500" aria-hidden="true" />
          </div>
        ))}
      </Card>
      <Pager page={page} totalPages={totalPages} onChange={setPage} />
    </div>
  );
}
