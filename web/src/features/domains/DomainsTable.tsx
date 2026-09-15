import { useMemo, useState } from "react";
import { MdChevronRight } from "react-icons/md";
import { Card } from "../../components/ui/Card";
import { Pager } from "../../components/ui/Pager";
import { Tag } from "../../components/ui/Tag";
import { EmptyState } from "../../layout/EmptyState";
import type { Domain } from "../../types/api";
import { useDomains } from "./hooks";
import { domainStatusLabel, domainStatusVariant, sslStatusLabel } from "./domainStatusMeta";

function formatTimestamp(iso: string | null): string {
  return iso ? new Date(iso).toLocaleString("pt-BR") : "—";
}

// attachedPageColumn renders spec.md DSP-02/03/04's "Aponta para" column:
// "—" when nothing is attached, the (single) attached page's name, or that
// name plus " +N" when more than one page is attached.
function attachedPageColumn(domain: Pick<Domain, "attached_page_name" | "attached_page_count">): string {
  if (!domain.attached_page_name || domain.attached_page_count === 0) return "—";
  const extra = domain.attached_page_count - 1;
  return extra > 0 ? `${domain.attached_page_name} +${extra}` : domain.attached_page_name;
}

export interface DomainsTableProps {
  /** Chamado quando uma linha da tabela é selecionada (abre o
   * DomainDetailDrawer, T7). */
  onSelect: (domain: Domain) => void;
}

/** Tabela redesenhada da aba Domínios (spec.md DSP-01): Status/Domínio/
 * Tipo/Aponta para/SSL/Verificado, mesma estrutura de grid/linha/Pager que
 * ServiceListPage já estabeleceu. */
export function DomainsTable({ onSelect }: DomainsTableProps) {
  const [page, setPage] = useState(1);
  const { data: domainsPage, isLoading } = useDomains(page);
  const domains = useMemo(() => domainsPage?.items ?? [], [domainsPage]);
  const totalPages = Math.max(1, Math.ceil((domainsPage?.total ?? 0) / (domainsPage?.page_size ?? 20)));

  if (isLoading) {
    return <p className="text-neutral-400">Carregando…</p>;
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
            <Tag variant={domainStatusVariant[domain.status]}>{domainStatusLabel[domain.status]}</Tag>
            <div className="min-w-0 truncate font-mono text-[13px] text-text">{domain.hostname}</div>
            <div className="text-[13px] text-neutral-300">Domínio próprio</div>
            <div className="min-w-0 truncate text-[13px] text-neutral-300">{attachedPageColumn(domain)}</div>
            <div className="text-[13px] text-neutral-300">{sslStatusLabel[domain.ssl_status]}</div>
            <div className="text-xs text-neutral-400">{formatTimestamp(domain.verified_at)}</div>
            <MdChevronRight size={16} className="text-neutral-500" aria-hidden="true" />
          </div>
        ))}
      </Card>
      <Pager page={page} totalPages={totalPages} onChange={setPage} />
    </div>
  );
}
