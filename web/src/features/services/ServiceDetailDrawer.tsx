import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import * as RadixDialog from "@radix-ui/react-dialog";
import { MdOutlineWarningAmber, MdClose, MdOutlineEdit, MdCheck } from "react-icons/md";
import { useAuth } from "../../auth/AuthProvider";
import { ApiError } from "../../lib/apiClient";
import { Input } from "../../components/ui/Input";
import type { HourlyBucket } from "../../types/api";
import { useServiceDetail, useUpdateService } from "./hooks";
import { StatusTag } from "./StatusTag";
import { serviceSubtitle } from "./statusMeta";

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

/** Drawer com os stats de 30d, nota de degradação condicional e a faixa de
 * 24 barras de status (SVC-14..19), mais a ação de renomear (service-edit
 * SVCEDIT-06, ownerOnly). Ainda sem "Pausar monitoramento" nem "Editar
 * configuração" (monitor_mode/slo_id/poll_*) - fora de escopo de
 * service-edit, ver seu spec.md.
 *
 * Não usa o <Drawer> compartilhado: seu título/descrição/rodapé sempre
 * vêm dentro de faixas com borda (border-b no cabeçalho, border-t no
 * rodapé) - o mock (`Servicos Monitorados.dc.html`'s `hasSelected` panel)
 * não tem nenhuma das duas, é um painel contínuo com badge+X no topo e
 * sem botão de rodapé nenhum. Radix Dialog usado diretamente para
 * reproduzir essa estrutura; `RadixDialog.Title` fica sempre sr-only
 * (o nome do serviço é renderizado visualmente como h2 ou como o input de
 * edição, nunca os dois ao mesmo tempo). */
export function ServiceDetailDrawer({ serviceId, onClose }: ServiceDetailDrawerProps) {
  const { t } = useTranslation();
  const { hasRole } = useAuth();
  const canRename = hasRole(["owner"]);
  const { data: detail, isLoading } = useServiceDetail(serviceId);
  const updateService = useUpdateService();

  const [editing, setEditing] = useState(false);
  const [nameDraft, setNameDraft] = useState("");
  const [renameError, setRenameError] = useState<string | null>(null);

  useEffect(() => {
    setEditing(false);
    setRenameError(null);
  }, [serviceId]);

  function startEditing() {
    setNameDraft(detail?.name ?? "");
    setRenameError(null);
    setEditing(true);
  }

  async function saveRename() {
    const trimmed = nameDraft.trim();
    if (!trimmed) {
      setRenameError(t("services.detail.nameRequired"));
      return;
    }
    setRenameError(null);
    try {
      await updateService.mutateAsync({ id: serviceId, name: trimmed });
      setEditing(false);
    } catch (err) {
      setRenameError(err instanceof ApiError ? err.message : t("services.detail.renameError"));
    }
  }

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
                <RadixDialog.Title className="sr-only">{t("services.detail.detailTitle")}</RadixDialog.Title>
                {editing ? (
                  <div className="flex items-center gap-1.5">
                    <Input
                      autoFocus
                      value={nameDraft}
                      onChange={(e) => setNameDraft(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === "Enter") void saveRename();
                        if (e.key === "Escape") setEditing(false);
                      }}
                      aria-label={t("services.detail.namePlaceholder")}
                      placeholder={t("services.detail.namePlaceholder")}
                      className="text-[15px] font-bold"
                      disabled={updateService.isPending}
                    />
                    <button
                      type="button"
                      onClick={() => void saveRename()}
                      disabled={updateService.isPending}
                      aria-label={t("services.detail.save")}
                      className="cursor-pointer text-success hover:opacity-80 disabled:opacity-50"
                    >
                      <MdCheck size={18} aria-hidden="true" />
                    </button>
                    <button
                      type="button"
                      onClick={() => setEditing(false)}
                      aria-label={t("services.detail.cancel")}
                      className="cursor-pointer text-neutral-400 hover:text-text"
                    >
                      <MdClose size={18} aria-hidden="true" />
                    </button>
                  </div>
                ) : (
                  <div className="flex items-center gap-1.5">
                    <h2 className="text-[19px] font-bold text-text">{detail.name}</h2>
                    {canRename ? (
                      <button
                        type="button"
                        onClick={startEditing}
                        aria-label={t("services.detail.editName")}
                        className="cursor-pointer text-neutral-400 hover:text-text"
                      >
                        <MdOutlineEdit size={15} aria-hidden="true" />
                      </button>
                    ) : null}
                  </div>
                )}
                {renameError ? (
                  <p role="alert" className="mt-1 text-xs text-critical">
                    {renameError}
                  </p>
                ) : null}
                <p className="mt-0.5 text-[13px] text-neutral-400">{serviceSubtitle(detail)}</p>
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
