import { useState } from "react";
import { useTranslation } from "react-i18next";
import { MdOutlineSchedule, MdOutlineAdd } from "react-icons/md";
import { Card } from "../../components/ui/Card";
import { Button } from "../../components/ui/Button";
import { Pager } from "../../components/ui/Pager";
import { Tag } from "../../components/ui/Tag";
import { Skeleton } from "../../components/ui/Skeleton";
import { useAuth } from "../../auth/AuthProvider";
import type { Service } from "../../types/api";
import { useServices } from "./hooks";
import { statusLabel, statusVariant, statusDotColor } from "./statusMeta";
import { AddServiceDrawer } from "./AddServiceDrawer";

function formatTimestamp(iso: string): string {
  return new Date(iso).toLocaleString("pt-BR");
}

function ClockIcon() {
  return <MdOutlineSchedule size={12} aria-hidden="true" />;
}

/** Tabela + drawer de vínculo de serviço a SLO. Compartilhada entre `IntegrationsPage` (handoff mostra as duas seções na mesma tela) e `ServicesPage` (rota própria, decisão registrada em design.md). */
export function ServicesSection() {
  const { t } = useTranslation();
  const { hasRole } = useAuth();
  const canManage = hasRole(["owner", "operator"]);
  const [page, setPage] = useState(1);
  const { data: servicesPage, isLoading } = useServices(page);
  const services = servicesPage?.items;
  const totalPages = Math.max(1, Math.ceil((servicesPage?.total ?? 0) / (servicesPage?.page_size ?? 20)));

  const [drawerOpen, setDrawerOpen] = useState(false);

  function serviceLastChange(s: Service): string {
    return s.current_status === "not_configured" ? "—" : formatTimestamp(s.last_status_change_at);
  }

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between">
        <h2 className="text-text">Serviços monitorados</h2>
        {canManage ? (
          <Button variant="primary" onClick={() => setDrawerOpen(true)}>
            <MdOutlineAdd size={14} aria-hidden="true" />
            Vincular serviço
          </Button>
        ) : null}
      </div>

      <div>
        {isLoading ? (
          <div aria-busy="true" className="rounded-md border border-divider divide-y divide-divider overflow-hidden">
            <span className="sr-only">{t("services.loading")}</span>
            {Array.from({ length: 3 }).map((_, i) => (
              <div key={i} className="flex items-center gap-3 px-4 py-3.5">
                <Skeleton width={8} height={8} radius={999} />
                <div className="flex-1">
                  <Skeleton width={140} height={14} />
                  <Skeleton width={100} height={12} className="mt-1.5" />
                </div>
                <Skeleton width={90} height={28} />
                <Skeleton width={70} height={20} radius={999} />
              </div>
            ))}
          </div>
        ) : (
          <>
            <Card elevation="none" className="border border-divider divide-y divide-divider overflow-hidden">
              {(services ?? []).length === 0 ? (
                <p className="px-4 py-6 text-center text-neutral-400">Nenhum serviço cadastrado.</p>
              ) : (
                (services ?? []).map((s) => (
                  <div key={s.id} data-testid="service-row" className="flex items-center gap-3 px-4 py-3.5">
                    <span
                      className="h-2 w-2 flex-none rounded-full"
                      style={{ backgroundColor: statusDotColor[s.current_status] }}
                      aria-hidden="true"
                    />
                    <div className="flex-1">
                      <div className="text-[15px] font-medium text-text">{s.name}</div>
                      <div className="mt-0.5 flex items-center gap-1 text-xs text-neutral-400">
                        <ClockIcon />
                        {s.slo_name ?? "—"}
                      </div>
                    </div>
                    <div className="text-right text-xs text-neutral-400">
                      <div>Última mudança</div>
                      <div className="mt-0.5 text-[13px] text-text">{serviceLastChange(s)}</div>
                    </div>
                    <Tag variant={statusVariant[s.current_status]}>{statusLabel[s.current_status]}</Tag>
                  </div>
                ))
              )}
            </Card>
            <Pager page={page} totalPages={totalPages} onChange={setPage} />
          </>
        )}
      </div>

      <AddServiceDrawer open={drawerOpen} onOpenChange={setDrawerOpen} />
    </div>
  );
}
