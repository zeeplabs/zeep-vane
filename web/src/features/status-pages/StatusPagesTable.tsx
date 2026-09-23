import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { MdChevronRight } from "react-icons/md";
import { Card } from "../../components/ui/Card";
import { Pager } from "../../components/ui/Pager";
import { Tag } from "../../components/ui/Tag";
import { Skeleton } from "../../components/ui/Skeleton";
import { EmptyState } from "../../layout/EmptyState";
import { formatDateTime } from "../../lib/formatDate";
import type { StatusPage } from "../../types/api";
import { useDomains } from "../domains/hooks";
import { useStatusPages } from "./hooks";

// publicUrl composes "Public URL" (spec.md DSP-13/17) from the page's own
// domain_id/subdomain plus the attached domain's hostname - "—" when no
// domain is attached, mirroring StatusPagesSection.tsx's existing
// publicUrl()/DSP-17 null-safety guard.
function publicUrl(page: StatusPage, hostname: string | undefined): string {
  if (!page.domain_id || !page.subdomain || !hostname) return "—";
  return `${page.subdomain}.${hostname}`;
}

export interface StatusPagesTableProps {
  /** Called when a table row is selected (opens the
   * StatusPageDetailDrawer, T10). */
  onSelect: (page: StatusPage) => void;
}

/** Redesigned table for the Status Pages tab (spec.md DSP-13): Visib./Page/
 * Public URL/Services/Updated, same grid/row/Pager structure that
 * DomainsTable (T6) already established. */
export function StatusPagesTable({ onSelect }: StatusPagesTableProps) {
  const { t, i18n } = useTranslation();
  const [page, setPage] = useState(1);
  const { data: statusPagesPage, isLoading } = useStatusPages(page);
  const pages = useMemo(() => statusPagesPage?.items ?? [], [statusPagesPage]);
  const totalPages = Math.max(1, Math.ceil((statusPagesPage?.total ?? 0) / (statusPagesPage?.page_size ?? 20)));

  // SPEC_DEVIATION: fixed page 1 for now - the domains dropdown here only
  // resolves a hostname for the "Public URL" column, not a picker; Pager UI
  // for it is out of scope. Mirrors the same deviation in
  // StatusPagesSection.tsx.
  const { data: domainsPage } = useDomains(1);
  const domains = domainsPage?.items;

  function hostnameFor(p: StatusPage): string | undefined {
    return domains?.find((d) => d.id === p.domain_id)?.hostname;
  }

  if (isLoading) {
    return (
      <div aria-busy="true" className="overflow-hidden rounded-md border border-divider">
        <span className="sr-only">{t("statusPages.loading")}</span>
        {Array.from({ length: 5 }).map((_, i) => (
          <div
            key={i}
            className="grid grid-cols-[100px_1fr_1fr_120px_140px_20px] items-center gap-3 border-b border-divider px-5 py-3.5 last:border-b-0"
          >
            <Skeleton width={70} height={20} radius={999} />
            <Skeleton width={140} height={14} />
            <Skeleton width={160} height={14} />
            <Skeleton width={70} height={14} />
            <Skeleton width={90} height={12} />
            <Skeleton width={16} height={16} />
          </div>
        ))}
      </div>
    );
  }

  if (pages.length === 0) {
    return <EmptyState title={t("statusPages.empty.title")} description={t("statusPages.empty.description")} />;
  }

  return (
    <div className="flex flex-col gap-3">
      <Card elevation="none" className="overflow-hidden border border-divider">
        <div className="grid grid-cols-[100px_1fr_1fr_120px_140px_20px] items-center gap-3 border-b border-divider bg-card-header-bg px-5 py-2.5">
          <span className="text-[11px] font-bold uppercase tracking-wide text-neutral-400">{t("statusPages.table.visibility")}</span>
          <span className="text-[11px] font-bold uppercase tracking-wide text-neutral-400">{t("statusPages.table.page")}</span>
          <span className="text-[11px] font-bold uppercase tracking-wide text-neutral-400">{t("statusPages.table.publicUrl")}</span>
          <span className="text-[11px] font-bold uppercase tracking-wide text-neutral-400">{t("statusPages.table.services")}</span>
          <span className="text-[11px] font-bold uppercase tracking-wide text-neutral-400">{t("statusPages.table.updated")}</span>
          <span />
        </div>
        {pages.map((p) => (
          <div
            key={p.id}
            data-testid="status-page-row"
            onClick={() => onSelect(p)}
            className="grid cursor-pointer grid-cols-[100px_1fr_1fr_120px_140px_20px] items-center gap-3 border-b border-divider px-5 py-3.5 last:border-b-0 hover:bg-card-header-bg"
          >
            <Tag variant="success">{t("statusPages.publicTag")}</Tag>
            <div className="min-w-0 truncate text-[13px] text-text">{p.name}</div>
            <div className="min-w-0 truncate font-mono text-[13px] font-medium text-text-muted">{publicUrl(p, hostnameFor(p))}</div>
            <div className="text-[13px] font-medium text-text-muted">{t("statusPages.serviceCount", { count: p.service_ids.length })}</div>
            <div className="text-xs text-text-muted">{formatDateTime(p.created_at, i18n.language)}</div>
            <MdChevronRight size={16} className="text-neutral-500" aria-hidden="true" />
          </div>
        ))}
      </Card>
      <Pager page={page} totalPages={totalPages} onChange={setPage} />
    </div>
  );
}
