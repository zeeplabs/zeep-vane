import { useMemo, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { MdOutlineAdd, MdChevronRight, MdCheck } from "react-icons/md";
import { Card } from "../../components/ui/Card";
import { Tag } from "../../components/ui/Tag";
import { Button } from "../../components/ui/Button";
import { Field } from "../../components/ui/Field";
import { Textarea } from "../../components/ui/Textarea";
import { Drawer, drawerFooterPrimaryStyle, drawerFooterSecondaryStyle } from "../../components/ui/Drawer";
import { Pager } from "../../components/ui/Pager";
import { Skeleton } from "../../components/ui/Skeleton";
import { useAuth } from "../../auth/AuthProvider";
import { ApiError } from "../../lib/apiClient";
import type { IncidentSeverity, IncidentStatus } from "../../types/api";
import { formatDateTimeShort } from "../../lib/formatDate";
import { useServices } from "../services/hooks";
import { useCreateIncident, useIncidents } from "./hooks";
import { IncidentDetailDrawer } from "./IncidentDetailDrawer";
import { IncidentStatusTag } from "./IncidentStatusTag";
import { incidentStatusLabel, formatDuration, severityColor, severityLabel } from "./incidentStatusMeta";

// Filter chips mirror the mock's Todos/Investigando/Monitorando/Resolvido
// row (handoff-new-layout/Incidentes.dc.html), plus "identified" - a real
// status the mock's seed data never exercises but the backend supports.
type StatusFilter = "all" | IncidentStatus;
const statusFilters: StatusFilter[] = ["all", "investigating", "identified", "monitoring", "resolved"];
const filterLabel: Record<StatusFilter, string> = { all: "Todos", ...incidentStatusLabel };

const severityOptions: { value: IncidentSeverity; label: string }[] = [
  { value: "minor", label: "Menor" },
  { value: "moderate", label: "Moderado" },
  { value: "critical", label: "Crítico" },
];

const DEFAULT_SEVERITY: IncidentSeverity = "moderate";

export function IncidentsPage() {
  const { t, i18n } = useTranslation();
  const { hasRole } = useAuth();
  const canManage = hasRole(["owner", "operator"]);
  const [statusFilter, setStatusFilter] = useState<StatusFilter>("all");
  // PAG-07/PAG-11: the incidents list is the one endpoint with unbounded
  // growth, so it is paginated (page_size 25) with a Pager below the list.
  // The filter chips keep filtering the currently-loaded page's items,
  // exactly as before pagination.
  const [page, setPage] = useState(1);
  const { data: incidentsPage, isLoading } = useIncidents(page);
  const incidents = useMemo(() => incidentsPage?.items ?? [], [incidentsPage]);
  const totalPages = Math.max(1, Math.ceil((incidentsPage?.total ?? 0) / (incidentsPage?.page_size ?? 25)));
  // SPEC_DEVIATION: fixed page 1 for now - Pager UI for the services
  // dropdown/lookup is out of scope here; T14/T16 (Pager) is a later
  // phase not yet built. Mirrors the same deviation in ServicesSection.tsx.
  const { data: servicesPage } = useServices(1);
  const services = servicesPage?.items;
  const createIncident = useCreateIncident();

  const [dialogOpen, setDialogOpen] = useState(false);
  const [title, setTitle] = useState("");
  const [serviceIds, setServiceIds] = useState<string[]>([]);
  const [severity, setSeverity] = useState<IncidentSeverity>(DEFAULT_SEVERITY);
  const [description, setDescription] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [selectedId, setSelectedId] = useState<string | null>(null);

  function toggleService(id: string) {
    setServiceIds((prev) => (prev.includes(id) ? prev.filter((s) => s !== id) : [...prev, id]));
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    try {
      await createIncident.mutateAsync({
        title,
        service_ids: serviceIds,
        severity,
        description: description.trim() ? description : undefined,
      });
      setTitle("");
      setServiceIds([]);
      setSeverity(DEFAULT_SEVERITY);
      setDescription("");
      setDialogOpen(false);
    } catch (err) {
      if (err instanceof ApiError) setError(err.message);
      else setError("Não foi possível criar o incidente.");
    }
  }

  const counts = useMemo(() => {
    const base: Record<StatusFilter, number> = { all: incidents.length, investigating: 0, identified: 0, monitoring: 0, resolved: 0 };
    for (const incident of incidents) base[incident.status] += 1;
    return base;
  }, [incidents]);

  const filtered = useMemo(
    () => (statusFilter === "all" ? incidents : incidents.filter((i) => i.status === statusFilter)),
    [incidents, statusFilter]
  );

  function serviceName(id: string): string {
    return services?.find((s) => s.id === id)?.name ?? id;
  }

  const selectedIncident = incidents.find((i) => i.id === selectedId) ?? null;

  return (
    <div className="mx-auto flex w-full max-w-[1280px] flex-col gap-6">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-text">Incidentes</h2>
          <p className="m-0 text-[13.5px] text-neutral-400">
            Acompanhe, atualize e encerre incidentes dos serviços monitorados.
          </p>
        </div>
        {canManage ? (
          <Button variant="solid" onClick={() => setDialogOpen(true)}>
            <MdOutlineAdd size={14} aria-hidden="true" />
            Novo incidente
          </Button>
        ) : null}
      </div>

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
                  ? "border-accent bg-accent-100 text-accent"
                  : "border-divider bg-surface text-neutral-400 hover:text-text")
              }
            >
              {filterLabel[value]}
              <span className="opacity-70">{counts[value]}</span>
            </button>
          );
        })}
      </div>

      {isLoading ? (
        <div aria-busy="true" className="overflow-hidden rounded-md border border-divider">
          <span className="sr-only">{t("incidents.loading")}</span>
          {Array.from({ length: 5 }).map((_, i) => (
            <div
              key={i}
              className="grid grid-cols-[130px_1fr_140px_100px_130px_90px_20px] items-center gap-3 border-b border-divider px-5 py-3.5 last:border-b-0"
            >
              <Skeleton width={90} height={20} radius={999} />
              <Skeleton width={200} height={14} />
              <Skeleton width={110} height={12} />
              <Skeleton width={70} height={12} />
              <Skeleton width={100} height={12} />
              <Skeleton width={60} height={12} />
              <Skeleton width={16} height={16} />
            </div>
          ))}
        </div>
      ) : (
        <Card elevation="none" className="overflow-hidden border border-divider">
          <div className="grid grid-cols-[130px_1fr_140px_100px_130px_90px_20px] items-center gap-3 border-b border-divider bg-card-header-bg px-5 py-2.5">
            <span className="text-[11px] font-bold uppercase tracking-wide text-neutral-400">Status</span>
            <span className="text-[11px] font-bold uppercase tracking-wide text-neutral-400">Incidente</span>
            <span className="text-[11px] font-bold uppercase tracking-wide text-neutral-400">Serviço</span>
            <span className="text-[11px] font-bold uppercase tracking-wide text-neutral-400">Severidade</span>
            <span className="text-[11px] font-bold uppercase tracking-wide text-neutral-400">Aberto em</span>
            <span className="text-[11px] font-bold uppercase tracking-wide text-neutral-400">Duração</span>
            <span />
          </div>
          {filtered.length === 0 ? (
            <p className="px-5 py-12 text-center text-[13.5px] text-neutral-400">
              Nenhum incidente encontrado com esse filtro.
            </p>
          ) : (
            filtered.map((incident) => (
              <div
                key={incident.id}
                data-testid="incident-row"
                onClick={() => setSelectedId(incident.id)}
                className="grid cursor-pointer grid-cols-[130px_1fr_140px_100px_130px_90px_20px] items-center gap-3 border-b border-divider px-5 py-3.5 last:border-b-0 hover:bg-card-header-bg"
              >
                <IncidentStatusTag status={incident.status} />
                <div className="flex min-w-0 items-center gap-2">
                  <span className="min-w-0 truncate text-[13.5px] font-bold text-text">{incident.title}</span>
                  {incident.auto_created ? <Tag variant="neutral-outline">Automático</Tag> : null}
                </div>
                <div className="min-w-0 truncate text-[12.5px] text-neutral-400">
                  {incident.service_ids.length > 0 ? incident.service_ids.map(serviceName).join(", ") : "—"}
                </div>
                <div className="text-[12.5px] font-bold" style={{ color: severityColor(incident.severity) }}>
                  {severityLabel(incident.severity)}
                </div>
                <div className="text-[12.5px] text-neutral-400">{formatDateTimeShort(incident.created_at, i18n.language)}</div>
                <div className="text-[12.5px] font-semibold text-text">
                  {formatDuration(incident.created_at, incident.resolved_at)}
                </div>
                <MdChevronRight size={16} className="text-neutral-500" aria-hidden="true" />
              </div>
            ))
          )}
        </Card>
      )}

      <Pager page={page} totalPages={totalPages} onChange={setPage} />

      <IncidentDetailDrawer
        incident={selectedIncident}
        canManage={canManage}
        serviceName={serviceName}
        onClose={() => setSelectedId(null)}
      />

      <Drawer
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        title="Criar incidente"
        description="Descreva o incidente e vincule os serviços afetados."
        closeLabel="Fechar"
        footer={
          <>
            <Button
              type="button"
              variant="secondary"
              style={drawerFooterSecondaryStyle}
              onClick={() => setDialogOpen(false)}
            >
              Cancelar
            </Button>
            <Button
              type="submit"
              form="create-incident-form"
              variant="solid"
              style={drawerFooterPrimaryStyle}
              disabled={createIncident.isPending}
            >
              Criar
            </Button>
          </>
        }
      >
        <form id="create-incident-form" onSubmit={handleSubmit} className="flex flex-col gap-3">
          <Field variant="filled" label="Título" value={title} onChange={(e) => setTitle(e.target.value)} required />
          <div className="flex flex-col gap-1">
            <span className="text-sm font-medium text-text">Serviços afetados</span>
            <div className="flex flex-col gap-1">
              {(services ?? []).map((s) => {
                const checked = serviceIds.includes(s.id);
                return (
                  <button
                    key={s.id}
                    type="button"
                    onClick={() => toggleService(s.id)}
                    aria-pressed={checked}
                    className="flex cursor-pointer items-center gap-2.5 rounded-md px-2.5 py-[9px] text-left hover:bg-card-header-bg"
                  >
                    <span
                      className={
                        "flex h-[16px] w-[16px] flex-none items-center justify-center rounded-[5px] border-[1.5px] " +
                        (checked ? "border-accent bg-accent" : "border-divider bg-surface")
                      }
                    >
                      {checked ? <MdCheck size={11} className="text-white" aria-hidden="true" /> : null}
                    </span>
                    <span className="text-[13px] font-semibold text-text">{s.name}</span>
                  </button>
                );
              })}
            </div>
          </div>
          <div className="flex flex-col gap-1">
            <span className="text-sm font-medium text-text">Severidade</span>
            <div role="radiogroup" aria-label="Severidade" className="grid grid-cols-3 gap-2">
              {severityOptions.map((opt) => {
                const active = severity === opt.value;
                const color = severityColor(opt.value);
                return (
                  <button
                    key={opt.value}
                    type="button"
                    role="radio"
                    aria-checked={active}
                    onClick={() => setSeverity(opt.value)}
                    className="cursor-pointer rounded-md border-[1.5px] px-3 py-2.5 text-center text-[12.5px] font-bold transition-colors"
                    style={
                      active
                        ? { borderColor: color, backgroundColor: `color-mix(in oklch, ${color} 12%, transparent)`, color }
                        : { borderColor: "var(--color-divider)", backgroundColor: "var(--color-surface)", color: "var(--color-text-muted)" }
                    }
                  >
                    {opt.label}
                  </button>
                );
              })}
            </div>
          </div>
          <div className="flex flex-col gap-1">
            <label htmlFor="incident-description" className="text-sm font-medium text-text">
              Descrição inicial
            </label>
            <Textarea
              id="incident-description"
              variant="filled"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="O que está acontecendo?"
              rows={4}
            />
          </div>
          {error ? (
            <p role="alert" className="text-xs text-critical">
              {error}
            </p>
          ) : null}
        </form>
      </Drawer>
    </div>
  );
}
