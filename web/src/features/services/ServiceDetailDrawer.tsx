import { useTranslation } from "react-i18next";
import { MdOutlineWarningAmber } from "react-icons/md";
import { Drawer } from "../../components/ui/Drawer";
import { Button } from "../../components/ui/Button";
import { Tag } from "../../components/ui/Tag";
import type { HourlyBucket } from "../../types/api";
import { useServiceDetail } from "./hooks";
import { statusLabel, statusVariant } from "./statusMeta";

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
 * (spec.md Out of Scope). */
export function ServiceDetailDrawer({ serviceId, onClose }: ServiceDetailDrawerProps) {
  const { t } = useTranslation();
  const { data: detail, isLoading } = useServiceDetail(serviceId);

  const showNote = detail?.current_status === "degraded" && !!detail?.status_analysis;

  return (
    <Drawer
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
      title={detail?.name ?? "…"}
      description={detail?.slo_name || detail?.slo_id || undefined}
      footer={
        <Button type="button" variant="secondary" onClick={onClose}>
          {t("services.detail.close")}
        </Button>
      }
    >
      {isLoading || !detail ? (
        <p className="text-neutral-400">{t("services.loading")}</p>
      ) : (
        <div className="flex flex-col gap-5">
          <Tag variant={statusVariant[detail.current_status]}>{statusLabel[detail.current_status]}</Tag>

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
    </Drawer>
  );
}
