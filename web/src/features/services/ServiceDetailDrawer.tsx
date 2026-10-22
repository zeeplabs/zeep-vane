import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import * as RadixDialog from "@radix-ui/react-dialog";
import { MdOutlineWarningAmber, MdClose, MdOutlineEdit, MdCheck, MdOutlineDeleteOutline } from "react-icons/md";
import { useAuth } from "../../auth/AuthProvider";
import { ApiError } from "../../lib/apiClient";
import { formatDateTime } from "../../lib/formatDate";
import { Input } from "../../components/ui/Input";
import { Button } from "../../components/ui/Button";
import { Dialog } from "../../components/ui/Dialog";
import type { HourlyBucket, ServiceEnvironment, ServiceLatestReading } from "../../types/api";
import { formatLastAccess } from "../admins/adminMeta";
import { EnvironmentTag } from "./EnvironmentTag";
import { environmentLabel, environmentOptions } from "./environmentMeta";
import { useDeleteService, useServiceDetail, useUpdateService } from "./hooks";
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

function formatUptime(uptime: number | null): string {
  return uptime === null ? "—" : `${uptime.toFixed(2)}%`;
}

// formatBurnRate renders a burn rate as "12.3x" (one decimal, same
// toFixed convention as formatUptime). The API sends null only for a
// non-finite burn (SLO target 100% with errors in the window), shown as
// "∞"; `unbounded` lets the caller fall back to "—" when the window has no
// errors and so cannot be unbounded.
function formatBurnRate(value: number | null, unbounded: boolean): string {
  if (value === null) return unbounded ? "∞" : "—";
  return `${value.toFixed(1)}x`;
}

interface StatProps {
  label: string;
  value: string;
  hint?: string;
}

function Stat({ label, value, hint }: StatProps) {
  return (
    <div>
      <div className="mb-1 text-[10.5px] font-bold uppercase tracking-wide text-neutral-400">{label}</div>
      <div className="text-lg font-bold text-text">{value}</div>
      {hint ? <div className="text-[11.5px] text-neutral-400">{hint}</div> : null}
    </div>
  );
}

interface LatestReadingProps {
  reading: ServiceLatestReading | null;
}

/** "Última leitura" block (poller-burn-rate-classification BURN-14): the
 * error rate and burn rates the poller computed on its last cycle, so an
 * admin can see why the service has its current status. window_errors/
 * window_requests are the short (5 min) window's counts. */
function LatestReading({ reading }: LatestReadingProps) {
  const { t, i18n } = useTranslation();

  const header = (
    <div className="mb-2 flex items-baseline justify-between gap-2">
      <div className="text-[10.5px] font-bold uppercase tracking-wide text-neutral-400">
        {t("services.detail.latestReading.title")}
      </div>
      {reading ? (
        <time dateTime={reading.read_at} className="text-[11.5px] text-neutral-400">
          {formatLastAccess(reading.read_at, t)}
        </time>
      ) : null}
    </div>
  );

  if (!reading) {
    return (
      <section>
        {header}
        <p className="text-[12.5px] text-neutral-400">{t("services.detail.latestReading.empty")}</p>
      </section>
    );
  }

  const { window_errors: errors, window_requests: requests } = reading;
  const noTraffic = requests === 0;
  const errorRate = noTraffic
    ? t("services.detail.latestReading.noTraffic")
    : `${((errors / requests) * 100).toFixed(2)}%`;
  const errorsInRequests = noTraffic
    ? undefined
    : t("services.detail.latestReading.errorsInRequests", {
        errors: t("services.detail.latestReading.errorCount", {
          count: errors,
          value: errors.toLocaleString(i18n.language),
        }),
        requests: t("services.detail.latestReading.requestCount", {
          count: requests,
          value: requests.toLocaleString(i18n.language),
        }),
      });

  return (
    <section>
      {header}
      <div className="grid grid-cols-3 gap-4">
        <Stat label={t("services.detail.latestReading.errorRate")} value={errorRate} hint={errorsInRequests} />
        <Stat
          label={t("services.detail.latestReading.burnShort")}
          value={formatBurnRate(reading.burn_rate_short, errors > 0)}
        />
        {/* The long (1 h) window's error count isn't exposed, but a null
         * burn_rate_long can only mean +Inf (the backend never sends NaN),
         * so it always renders as "∞". */}
        <Stat label={t("services.detail.latestReading.burnLong")} value={formatBurnRate(reading.burn_rate_long, true)} />
      </div>
      <p className="mt-2 text-[11.5px] text-neutral-400">{t("services.detail.latestReading.burnHelp")}</p>
    </section>
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
  const { t, i18n } = useTranslation();
  const { hasRole } = useAuth();
  const isOwner = hasRole(["owner"]);
  const { data: detail, isLoading } = useServiceDetail(serviceId);
  const updateService = useUpdateService();
  const deleteService = useDeleteService();

  const [editing, setEditing] = useState(false);
  const [nameDraft, setNameDraft] = useState("");
  const [renameError, setRenameError] = useState<string | null>(null);
  const [confirmingDelete, setConfirmingDelete] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);
  const [environmentError, setEnvironmentError] = useState<string | null>(null);

  useEffect(() => {
    setEditing(false);
    setRenameError(null);
    setConfirmingDelete(false);
    setDeleteError(null);
    setEnvironmentError(null);
  }, [serviceId]);

  // changeEnvironment (service-environment-tag SET-03/SET-05) auto-saves the
  // select's new value immediately - separate from the name's explicit
  // edit/save/cancel flow above, since it's a single-field admin-only tag
  // with no draft state to confirm.
  async function changeEnvironment(next: ServiceEnvironment) {
    if (!detail) return;
    setEnvironmentError(null);
    try {
      await updateService.mutateAsync({ id: serviceId, name: detail.name, environment: next });
    } catch (err) {
      setEnvironmentError(err instanceof ApiError ? err.message : t("services.detail.environmentError"));
    }
  }

  async function confirmDelete() {
    setDeleteError(null);
    try {
      await deleteService.mutateAsync(serviceId);
      setConfirmingDelete(false);
      onClose();
    } catch (err) {
      setDeleteError(err instanceof ApiError ? err.message : t("services.detail.deleteError"));
    }
  }

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
                    <EnvironmentTag environment={detail.environment} />
                    {isOwner ? (
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
                {isOwner ? (
                  <div className="mt-2 flex flex-col gap-1">
                    <label htmlFor="service-detail-environment" className="text-xs font-medium text-neutral-400">
                      {t("services.environment.label")}
                    </label>
                    <select
                      id="service-detail-environment"
                      value={detail.environment ?? ""}
                      onChange={(e) => void changeEnvironment(e.target.value as ServiceEnvironment)}
                      disabled={updateService.isPending}
                      className="min-h-8 w-fit rounded-md border border-transparent bg-card-header-bg px-2.5 text-xs text-text outline-none transition-colors focus:border-accent focus:bg-surface"
                    >
                      {environmentOptions.map((value) => (
                        <option key={value} value={value}>
                          {environmentLabel(t, value)}
                        </option>
                      ))}
                    </select>
                    {environmentError ? (
                      <p role="alert" className="text-xs text-critical">
                        {environmentError}
                      </p>
                    ) : null}
                  </div>
                ) : null}
              </div>

              <div className="grid grid-cols-2 gap-4">
                <Stat label={t("services.detail.uptime")} value={formatUptime(detail.uptime_30d)} />
                <Stat
                  label={t("services.detail.lastCheck")}
                  value={detail.last_seen_at ? formatDateTime(detail.last_seen_at, i18n.language) : "—"}
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

              {/* Polling-manual services never go through the Datadog
               * burn-rate path, so they have no reading to show. */}
              {detail.monitor_mode === "slo" ? <LatestReading reading={detail.latest_reading} /> : null}

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

              {isOwner ? (
                <Button
                  type="button"
                  variant="secondary"
                  style={{ borderColor: "var(--color-critical)", color: "var(--color-critical)" }}
                  onClick={() => {
                    setDeleteError(null);
                    setConfirmingDelete(true);
                  }}
                >
                  <MdOutlineDeleteOutline size={15} aria-hidden="true" />
                  {t("services.detail.deleteAction")}
                </Button>
              ) : null}
            </div>
          )}
        </RadixDialog.Content>
      </RadixDialog.Portal>

      {detail ? (
        <Dialog
          open={confirmingDelete}
          onOpenChange={(open) => {
            if (!open) setConfirmingDelete(false);
          }}
          title={t("services.detail.deleteConfirmTitle")}
          description={t("services.detail.deleteConfirmDescription", { name: detail.name })}
          footer={
            <>
              <Button variant="secondary" onClick={() => setConfirmingDelete(false)}>
                {t("services.detail.cancel")}
              </Button>
              <Button
                variant="primary"
                onClick={() => void confirmDelete()}
                disabled={deleteService.isPending}
                style={{ backgroundColor: "var(--color-critical)" }}
              >
                {t("services.detail.deleteAction")}
              </Button>
            </>
          }
        >
          {deleteError ? (
            <p role="alert" className="text-xs text-critical">
              {deleteError}
            </p>
          ) : null}
        </Dialog>
      ) : null}
    </RadixDialog.Root>
  );
}
