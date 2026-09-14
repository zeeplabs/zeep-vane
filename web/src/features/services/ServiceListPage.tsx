import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { MdOutlineAdd, MdOutlineSearch, MdChevronRight } from "react-icons/md";
import { Card } from "../../components/ui/Card";
import { Button } from "../../components/ui/Button";
import { Input } from "../../components/ui/Input";
import { Pager } from "../../components/ui/Pager";
import { Tag } from "../../components/ui/Tag";
import type { ServiceStatus } from "../../types/api";
import { useServices } from "./hooks";
import { statusLabel, statusVariant } from "./statusMeta";

type StatusFilter = "all" | ServiceStatus;

const statusFilters: StatusFilter[] = ["all", "operational", "degraded", "outage", "not_configured"];

const filterI18nKey: Record<StatusFilter, string> = {
  all: "services.filters.all",
  operational: "services.filters.operational",
  degraded: "services.filters.degraded",
  outage: "services.filters.outage",
  not_configured: "services.filters.notConfigured",
};

function formatTimestamp(iso: string): string {
  return new Date(iso).toLocaleString("pt-BR");
}

function formatUptime(uptime: number | null): string {
  return uptime === null ? "—" : `${uptime.toFixed(2)}%`;
}

export interface ServiceListPageProps {
  /** Chamado quando uma linha da tabela é selecionada (abre o drawer de detalhe, T10/T11). */
  onSelectService?: (id: string) => void;
  /** Chamado ao clicar em "Adicionar serviço" (abre o AddServiceDrawer, T11). */
  onAddService?: () => void;
}

/** Tela redesenhada de `/services` (monitored-services-page): tabela com
 * status/uptime/última verificação, chips de filtro por status e busca por
 * nome/SLO, todos escopados à página atual (SVC-01..13). */
export function ServiceListPage({ onSelectService, onAddService }: ServiceListPageProps) {
  const { t } = useTranslation();
  const [page, setPage] = useState(1);
  const [statusFilter, setStatusFilter] = useState<StatusFilter>("all");
  const [searchQuery, setSearchQuery] = useState("");

  const { data: servicesPage, isLoading } = useServices(page);
  const services = useMemo(() => servicesPage?.items ?? [], [servicesPage]);
  const totalPages = Math.max(1, Math.ceil((servicesPage?.total ?? 0) / (servicesPage?.page_size ?? 20)));

  // Chip counts are scoped to the current page's raw items (SVC-13),
  // independent of the search box - mirroring the mock's own chip counts,
  // which are computed off the full page list, not the filtered view.
  const counts = useMemo(() => {
    const base: Record<StatusFilter, number> = {
      all: services.length,
      operational: 0,
      degraded: 0,
      outage: 0,
      not_configured: 0,
    };
    for (const service of services) {
      base[service.current_status] += 1;
    }
    return base;
  }, [services]);

  const filtered = useMemo(() => {
    const query = searchQuery.trim().toLowerCase();
    return services.filter((service) => {
      const matchesStatus = statusFilter === "all" || service.current_status === statusFilter;
      const matchesQuery =
        !query ||
        service.name.toLowerCase().includes(query) ||
        (service.slo_name ?? "").toLowerCase().includes(query);
      return matchesStatus && matchesQuery;
    });
  }, [services, statusFilter, searchQuery]);

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-lg font-bold text-text">{t("services.title")}</h1>
          <p className="mt-1.5 max-w-lg text-sm text-neutral-400">{t("services.subtitle")}</p>
        </div>
        <Button variant="primary" onClick={() => onAddService?.()}>
          <MdOutlineAdd size={15} aria-hidden="true" />
          {t("services.addButton")}
        </Button>
      </div>

      <div className="flex flex-wrap items-center justify-between gap-4">
        <div role="group" aria-label="Filtrar por status" className="flex flex-wrap items-center gap-2">
          {statusFilters.map((value) => {
            const active = statusFilter === value;
            return (
              <button
                key={value}
                type="button"
                aria-pressed={active}
                onClick={() => setStatusFilter(value)}
                className={
                  "inline-flex cursor-pointer items-center gap-1.5 rounded-full border px-3.5 py-1.5 text-xs font-semibold transition-colors " +
                  (active
                    ? "border-accent bg-accent-900 text-accent"
                    : "border-divider bg-surface text-neutral-400 hover:text-text")
                }
              >
                {t(filterI18nKey[value])}
                <span className="opacity-70">{counts[value]}</span>
              </button>
            );
          })}
        </div>
        <div className="relative w-60 flex-shrink-0">
          <MdOutlineSearch
            size={15}
            className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-neutral-500"
            aria-hidden="true"
          />
          <Input
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            placeholder={t("services.searchPlaceholder")}
            aria-label={t("services.searchPlaceholder")}
            className="pl-9"
          />
        </div>
      </div>

      {isLoading ? (
        <p className="text-neutral-400">{t("services.loading")}</p>
      ) : (
        <>
          <Card elevation="elev-sm" className="overflow-hidden">
            <div className="grid grid-cols-[96px_1fr_96px_140px_20px] items-center gap-3 border-b border-divider bg-bg px-5 py-2.5">
              <span className="text-[11px] font-bold uppercase tracking-wide text-neutral-400">
                {t("services.columns.status")}
              </span>
              <span className="text-[11px] font-bold uppercase tracking-wide text-neutral-400">
                {t("services.columns.service")}
              </span>
              <span className="text-[11px] font-bold uppercase tracking-wide text-neutral-400">
                {t("services.columns.uptime")}
              </span>
              <span className="text-[11px] font-bold uppercase tracking-wide text-neutral-400">
                {t("services.columns.lastCheck")}
              </span>
              <span />
            </div>
            {filtered.length === 0 ? (
              <p className="px-5 py-12 text-center text-sm text-neutral-400">{t("services.emptyFiltered")}</p>
            ) : (
              filtered.map((service) => (
                <div
                  key={service.id}
                  data-testid="service-row"
                  onClick={() => onSelectService?.(service.id)}
                  className="grid cursor-pointer grid-cols-[96px_1fr_96px_140px_20px] items-center gap-3 border-b border-divider px-5 py-3.5 last:border-b-0 hover:bg-bg"
                >
                  <Tag variant={statusVariant[service.current_status]}>
                    {statusLabel[service.current_status]}
                  </Tag>
                  <div className="min-w-0">
                    <div className="truncate text-[13.5px] font-semibold text-text">{service.name}</div>
                    <div className="truncate text-xs text-neutral-400">
                      {service.slo_name || service.slo_id}
                    </div>
                  </div>
                  <div className="text-[13px] font-semibold text-text">{formatUptime(service.uptime_30d)}</div>
                  <div className="text-xs text-neutral-400">
                    {service.last_seen_at ? formatTimestamp(service.last_seen_at) : "—"}
                  </div>
                  <MdChevronRight size={16} className="text-neutral-500" aria-hidden="true" />
                </div>
              ))
            )}
          </Card>
          <Pager page={page} totalPages={totalPages} onChange={setPage} />
        </>
      )}
    </div>
  );
}
