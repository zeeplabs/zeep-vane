import { useEffect, useState, type FormEvent, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { MdOutlineLink, MdOutlineMonitorHeart } from "react-icons/md";
import { Drawer, drawerFooterPrimaryStyle, drawerFooterSecondaryStyle } from "../../components/ui/Drawer";
import { Button } from "../../components/ui/Button";
import { Field } from "../../components/ui/Field";
import { ApiError } from "../../lib/apiClient";
import { useIntegrationStatus, useSLOSearch } from "../integrations/hooks";
import { useCreateService } from "./hooks";
import type { MonitorMode, PollType } from "../../types/api";

export interface AddServiceDrawerProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

// checkTypes drives the check-type chip order (manual-polling-monitoring
// T8/P3 AC1) - HTTP(S) first (the mock's own default), then TCP, then Ping.
const checkTypes: PollType[] = ["http", "tcp", "ping"];

// pollIntervalOptions mirrors the backend's validPollIntervalSeconds
// (services_handler.go) - 30s/1min/5min, no other value is accepted.
const pollIntervalOptions: Array<30 | 60 | 300> = [30, 60, 300];

/** Vínculo de um novo serviço a um SLO do Datadog (SVC-20..25) - extraído de
 * ServicesSection.tsx para ser reutilizado também por ServiceListPage. Copy
 * segue `handoff-new-layout/Servicos Monitorados.dc.html`'s add-service
 * drawer (title/description/button) - the earlier "Vincular serviço"/
 * "Salvar" wording was ServicesSection's own pre-redesign copy, kept by
 * mistake during T8's extraction; nothing tests those strings.
 *
 * Extended by manual-polling-monitoring's T8 with the "Como monitorar" mode
 * toggle (SLO vs. polling-manual): "Baseado em SLO" keeps every existing
 * field/behavior unchanged (default mode); "Polling manual" swaps in a
 * check-type selector, a target field (label/placeholder per type), and an
 * interval selector, and hides the SLO-search/"Fonte" fields entirely. The
 * disabled "New Relic" chip next to Datadog is purely decorative (no click
 * handler, `aria-disabled`, never sent in any request body) - the user's own
 * explicit scoping decision for this feature (spec.md Out of Scope). */
export function AddServiceDrawer({ open, onOpenChange }: AddServiceDrawerProps) {
  const { t } = useTranslation();
  const [name, setName] = useState("");
  const [query, setQuery] = useState("");
  const [selectedSlo, setSelectedSlo] = useState<{ id: string; name: string } | null>(null);
  const [monitorMode, setMonitorMode] = useState<MonitorMode>("slo");
  const [pollType, setPollType] = useState<PollType>("http");
  const [pollTarget, setPollTarget] = useState("");
  const [pollIntervalSeconds, setPollIntervalSeconds] = useState<30 | 60 | 300 | null>(null);
  const [error, setError] = useState<string | null>(null);

  const createService = useCreateService();
  const sloSearch = useSLOSearch(query);
  const integrationStatus = useIntegrationStatus();
  // "Baseado em SLO" needs a live Datadog integration to import a target
  // from - without one there is nothing to select (empty SLO search
  // forever), so the mode is blocked and "Polling manual" becomes the only
  // usable option (the user's own explicit product decision, not an
  // inferred default).
  const datadogConnected = integrationStatus.data?.connected === true && integrationStatus.data.status === "active";
  const sloModeDisabled = integrationStatus.data !== undefined && !datadogConnected;

  useEffect(() => {
    if (sloModeDisabled && monitorMode === "slo") setMonitorMode("polling");
  }, [sloModeDisabled, monitorMode]);

  function resetForm() {
    setName("");
    setQuery("");
    setSelectedSlo(null);
    setMonitorMode(sloModeDisabled ? "polling" : "slo");
    setPollType("http");
    setPollTarget("");
    setPollIntervalSeconds(null);
    setError(null);
  }

  function handleOpenChange(next: boolean) {
    if (!next) resetForm();
    onOpenChange(next);
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);

    if (monitorMode === "slo") {
      if (!selectedSlo) {
        // The real backend requires slo_id on creation (SPEC_DEVIATION, I15:
        // the earlier mock allowed a service with no SLO at all) - validated
        // client-side so the admin gets an immediate, specific message
        // instead of a generic 422 from the API.
        setError("Selecione um SLO da lista antes de salvar.");
        return;
      }
      try {
        await createService.mutateAsync({ name, slo_id: selectedSlo.id, slo_name: selectedSlo.name });
        resetForm();
        onOpenChange(false);
      } catch (err) {
        if (err instanceof ApiError) setError(err.message);
        else setError("Não foi possível vincular o serviço.");
      }
      return;
    }

    // monitorMode === "polling": submit only this mode's fields (never
    // slo_id/slo_name - T8's own "Done when" contract).
    if (!pollTarget.trim() || pollIntervalSeconds === null) {
      setError(t("services.addDrawer.pollingValidationError"));
      return;
    }
    try {
      await createService.mutateAsync({
        name,
        monitor_mode: "polling",
        poll_type: pollType,
        poll_target: pollTarget,
        poll_interval_seconds: pollIntervalSeconds,
      });
      resetForm();
      onOpenChange(false);
    } catch (err) {
      if (err instanceof ApiError) setError(err.message);
      else setError("Não foi possível vincular o serviço.");
    }
  }

  const canSubmit =
    name.trim().length > 0 &&
    (monitorMode === "slo"
      ? selectedSlo !== null
      : pollTarget.trim().length > 0 && pollIntervalSeconds !== null);

  const targetLabel = t(`services.addDrawer.targetLabel.${pollType}`);
  const targetPlaceholder = t(`services.addDrawer.targetPlaceholder.${pollType}`);

  return (
    <Drawer
      open={open}
      onOpenChange={handleOpenChange}
      title="Adicionar serviço"
      description="Configure um novo alvo de monitoramento. O Vane começa a verificar assim que você salvar."
      closeLabel={t("common.close")}
      footer={
        <>
          <Button
            type="button"
            variant="secondary"
            style={drawerFooterSecondaryStyle}
            onClick={() => handleOpenChange(false)}
          >
            Cancelar
          </Button>
          <Button
            type="submit"
            form="add-service-form"
            variant="solid"
            style={drawerFooterPrimaryStyle}
            disabled={createService.isPending || !canSubmit}
          >
            Adicionar serviço
          </Button>
        </>
      }
    >
      <form id="add-service-form" onSubmit={handleSubmit} className="flex flex-col gap-[18px]">
        <Field
          variant="filled"
          label="Nome do serviço"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="Ex: Payments API"
          required
        />

        <div className="flex flex-col gap-[6px]">
          <span className="text-sm font-medium text-text">{t("services.addDrawer.monitorModeLabel")}</span>
          <div role="group" aria-label={t("services.addDrawer.monitorModeLabel")} className="grid grid-cols-2 gap-[10px]">
            <ModeCard
              active={monitorMode === "slo"}
              disabled={sloModeDisabled}
              title={t("services.addDrawer.monitorModeSlo")}
              description={
                sloModeDisabled
                  ? t("services.addDrawer.monitorModeSloDisabledHint")
                  : t("services.addDrawer.monitorModeSloDescription")
              }
              icon={<MdOutlineLink size={18} />}
              onClick={() => setMonitorMode("slo")}
            />
            <ModeCard
              active={monitorMode === "polling"}
              icon={<MdOutlineMonitorHeart size={18} />}
              title={t("services.addDrawer.monitorModePolling")}
              description={t("services.addDrawer.monitorModePollingDescription")}
              onClick={() => setMonitorMode("polling")}
            />
          </div>
        </div>

        {monitorMode === "slo" ? (
          <>
            <div className="flex flex-col gap-[6px]">
              <span className="text-sm font-medium text-text">{t("services.addDrawer.sourceLabel")}</span>
              <div role="group" aria-label={t("services.addDrawer.sourceLabel")} className="flex flex-wrap gap-2">
                <ChipButton active>{t("services.addDrawer.sourceDatadog")}</ChipButton>
                <ChipButton
                  active={false}
                  disabled
                  aria-disabled="true"
                  title={t("services.addDrawer.sourceNewRelicBadge")}
                >
                  {t("services.addDrawer.sourceNewRelic")}
                </ChipButton>
              </div>
            </div>
            <Field
              variant="filled"
              label="Buscar SLO"
              value={query}
              onChange={(e) => {
                setQuery(e.target.value);
                setSelectedSlo(null);
              }}
              placeholder="Digite o nome do SLO"
            />
            <p className="rounded-[9px] border border-accent/20 bg-accent-100 px-3 py-2.5 text-[12px] leading-[1.5] text-neutral-400">
              {t("services.addDrawer.sloHelperNote")}
            </p>
            {query.trim() && sloSearch.data ? (
              <ul className="flex flex-col gap-1 rounded-md border border-divider bg-bg p-1">
                {sloSearch.data.length === 0 ? (
                  <li className="px-2 py-1.5 text-xs text-neutral-400">Nenhum SLO encontrado.</li>
                ) : (
                  sloSearch.data.map((slo) => (
                    <li key={slo.id}>
                      <button
                        type="button"
                        onClick={() => {
                          setSelectedSlo(slo);
                          setQuery(slo.name);
                        }}
                        className={
                          "w-full cursor-pointer rounded-sm px-2 py-1.5 text-left text-sm hover:bg-neutral-800 " +
                          (selectedSlo?.id === slo.id ? "text-accent" : "text-text")
                        }
                      >
                        {slo.name}
                      </button>
                    </li>
                  ))
                )}
              </ul>
            ) : null}
          </>
        ) : (
          <>
            <div className="flex flex-col gap-[6px]">
              <span className="text-sm font-medium text-text">{t("services.addDrawer.checkTypeLabel")}</span>
              <div role="group" aria-label={t("services.addDrawer.checkTypeLabel")} className="flex flex-wrap gap-2">
                {checkTypes.map((type) => (
                  <ChipButton key={type} active={pollType === type} onClick={() => setPollType(type)}>
                    {t(`services.addDrawer.checkType.${type}`)}
                  </ChipButton>
                ))}
              </div>
            </div>
            <Field
              variant="filled"
              label={targetLabel}
              value={pollTarget}
              onChange={(e) => setPollTarget(e.target.value)}
              placeholder={targetPlaceholder}
              required
            />
            <div className="flex flex-col gap-[6px]">
              <span className="text-sm font-medium text-text">{t("services.addDrawer.intervalLabel")}</span>
              <div
                role="group"
                aria-label={t("services.addDrawer.intervalLabel")}
                className="flex flex-wrap gap-2"
              >
                {pollIntervalOptions.map((seconds) => (
                  <ChipButton
                    key={seconds}
                    active={pollIntervalSeconds === seconds}
                    onClick={() => setPollIntervalSeconds(seconds)}
                  >
                    {t(`services.addDrawer.interval.${seconds}`)}
                  </ChipButton>
                ))}
              </div>
            </div>
          </>
        )}

        {error ? (
          <p role="alert" className="text-xs text-critical">
            {error}
          </p>
        ) : null}
      </form>
    </Drawer>
  );
}

interface ModeCardProps {
  active: boolean;
  disabled?: boolean;
  icon: ReactNode;
  title: string;
  description: string;
  onClick: () => void;
}

// ModeCard is the "Como monitorar" tile (icon + title + description),
// distinct from the pill-shaped ChipButton used for Fonte/check-type/
// interval - the mock (`handoff-new-layout/Servicos Monitorados.dc.html`'s
// `modeBoxStyle`) renders this selector as a bigger two-tile grid, not a
// chip group. `disabled` blocks "Baseado em SLO" when no Datadog
// integration is connected (nothing to import a target from) - same
// no-onClick-attached pattern as the "New Relic" chip, never just a
// CSS-only disabled look.
function ModeCard({ active, disabled, icon, title, description, onClick }: ModeCardProps) {
  return (
    <button
      type="button"
      aria-pressed={active && !disabled}
      aria-disabled={disabled}
      aria-label={title}
      onClick={disabled ? undefined : onClick}
      className={
        "flex flex-col items-start rounded-[10px] border p-[14px] text-left transition-colors " +
        (disabled
          ? "cursor-not-allowed border-divider bg-surface text-neutral-400 opacity-60"
          : "cursor-pointer " +
            (active
              ? "border-accent bg-accent-100 text-accent"
              : "border-divider bg-surface text-neutral-400 hover:text-text"))
      }
    >
      <span className="mb-[8px]">{icon}</span>
      <span className="mb-0.5 text-[13px] font-bold">{title}</span>
      <span className="text-[11.5px] leading-[1.4] text-neutral-400">{description}</span>
    </button>
  );
}

interface ChipButtonProps {
  active: boolean;
  disabled?: boolean;
  onClick?: () => void;
  children: ReactNode;
  "aria-disabled"?: "true";
  title?: string;
}

// ChipButton is the shared chip-toggle visual pattern already used by
// ServiceListPage.tsx's status filters (border/bg-tint group), reused here
// for the mode/source/check-type/interval toggles this drawer now needs
// (T8's own "Reuses" note). A disabled chip (the "New Relic" placeholder)
// renders with no onClick wired at all - `disabled` alone would still let a
// wrapping label/keyboard interaction reach it in some browsers, so the
// click handler itself is simply never attached, and `aria-disabled` marks
// it non-interactive for assistive tech without removing it from the tab
// order entirely (native `disabled` already does that for a <button>, kept
// here for explicitness per the spec's own wording).
function ChipButton({ active, disabled, onClick, children, ...rest }: ChipButtonProps) {
  return (
    <button
      type="button"
      aria-pressed={active}
      disabled={disabled}
      onClick={disabled ? undefined : onClick}
      className={
        "inline-flex cursor-pointer items-center gap-1.5 rounded-full border px-3.5 py-1.5 text-xs font-semibold transition-colors disabled:cursor-not-allowed disabled:opacity-50 " +
        (active
          ? "border-accent bg-accent-100 text-accent"
          : "border-divider bg-surface text-neutral-400 hover:text-text")
      }
      {...rest}
    >
      {children}
    </button>
  );
}
