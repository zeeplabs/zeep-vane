import { useTranslation } from "react-i18next";
import * as RadixDialog from "@radix-ui/react-dialog";
import { MdOutlineWarningAmber, MdClose } from "react-icons/md";
import type { HourlyBucket } from "../../types/api";
import { useServiceDetail } from "./hooks";
import { StatusTag } from "./StatusTag";

export interface ServiceDetailDrawerProps {
  serviceId: string;
  onClose: () => void;
}

const bucketColor: Record<HourlyBucket["status"], string> = {
  operational: "var(--color-success)",
  degraded: "var(--color-warning)",
  outage: "var(--color-critical)",
  no_data: "var(--color-neutral-600)",
};

function formatTimestamp(iso: string): string {
  return new Date(iso).toLocaleString("pt-BR");
}

function formatUptime(uptime: number | null): string {
  return uptime === null ? "—" : `${uptime.toFixed(2)}%`;
}

interface StatProps {
  label: string;
  value: string;
}

function Stat({ label, value }: StatProps) {
  return (
    <div>
      <div className="mb-1 text-[10.5px] font-bold uppercase tracking-wide text-neutral-400">{label}</div>
      <div className="text-lg font-bold text-text">{value}</div>
    </div>
  );
}

/** Drawer somente-leitura com os stats de 30d, nota de degradação condicional
 * e a faixa de 24 barras de status (SVC-14..19). Sem "Pausar monitoramento"
 * nem "Editar configuração" - nenhum dos dois tem suporte no backend hoje
 * (spec.md Out of Scope).
 *
 * Não usa o <Drawer> compartilhado: seu título/descrição/rodapé sempre
 * vêm dentro de faixas com borda (border-b no cabeçalho, border-t no
 * rodapé) - o mock (`Servicos Monitorados.dc.html`'s `hasSelected` panel)
 * não tem nenhuma das duas, é um painel contínuo com badge+X no topo e
 * sem botão de rodapé nenhum. Radix Dialog usado diretamente para
 * reproduzir essa estrutura; `RadixDialog.Title` fica visualmente oculto
 * (sr-only) porque o nome do serviço já é renderizado como h2 visível. */
export function ServiceDetailDrawer({ serviceId, onClose }: ServiceDetailDrawerProps) {
  const { t } = useTranslation();
  const { data: detail, isLoading } = useServiceDetail(serviceId);

  const showNote = detail?.current_status === "degraded" && !!detail?.status_analysis;

  return (
    <RadixDialog.Root
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <RadixDialog.Portal>
        <RadixDialog.Overlay className="fixed inset-0 z-40 bg-black/60" />
        <RadixDialog.Content className="fixed right-0 top-0 z-50 flex h-full w-full max-w-md flex-col overflow-y-auto bg-surface p-6 shadow-lg outline-none">
          {isLoading || !detail ? (
            <>
              <RadixDialog.Title className="sr-only">…</RadixDialog.Title>
              <p className="text-neutral-400">{t("services.loading")}</p>
            </>
          ) : (
            <div className="flex flex-col gap-5">
              <div className="flex items-start justify-between">
                <StatusTag status={detail.current_status} />
                <button
                  type="button"
                  onClick={onClose}
                  aria-label={t("services.detail.close")}
                  className="cursor-pointer text-neutral-400 hover:text-text"
                >
                  <MdClose size={18} aria-hidden="true" />
                </button>
              </div>

              <div className="-mt-1">
                <RadixDialog.Title asChild>
                  <h2 className="text-[19px] font-bold text-text">{detail.name}</h2>
                </RadixDialog.Title>
                <p className="mt-0.5 text-[13px] text-neutral-400">{detail.slo_name || detail.slo_id}</p>
              </div>

              <div className="grid grid-cols-2 gap-4">
                <Stat label={t("services.detail.uptime")} value={formatUptime(detail.uptime_30d)} />
                <Stat
                  label={t("services.detail.lastCheck")}
                  value={detail.last_seen_at ? formatTimestamp(detail.last_seen_at) : "—"}
                />
                <Stat label={t("services.detail.incidents")} value={String(detail.incidents_30d)} />
              </div>

              {showNote ? (
                <div className="flex gap-2.5 rounded-md border border-warning/30 bg-warning/10 px-3.5 py-3">
                  <MdOutlineWarningAmber
                    size={16}
                    className="mt-0.5 flex-shrink-0 text-warning"
                    aria-hidden="true"
                  />
                  <p className="text-[12.5px] leading-relaxed text-neutral-300">{detail.status_analysis}</p>
                </div>
              ) : null}

              <div>
                <div className="mb-2 text-[10.5px] font-bold uppercase tracking-wide text-neutral-400">
                  {t("services.detail.history")}
                </div>
                <div className="flex gap-[3px]">
                  {detail.hourly_buckets.map((bucket, index) => (
                    <div
                      key={`${bucket.start}-${index}`}
                      data-testid="history-bar"
                      className="h-[22px] w-[12px] rounded-sm"
                      style={{ backgroundColor: bucketColor[bucket.status] }}
                      title={bucket.start}
                    />
                  ))}
                </div>
              </div>
            </div>
          )}
        </RadixDialog.Content>
      </RadixDialog.Portal>
    </RadixDialog.Root>
  );
}
