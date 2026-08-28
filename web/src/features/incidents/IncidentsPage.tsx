import { useState, type FormEvent } from "react";
import { Seg } from "../../components/ui/Seg";
import { Card } from "../../components/ui/Card";
import { Tag } from "../../components/ui/Tag";
import { Button } from "../../components/ui/Button";
import { Field } from "../../components/ui/Field";
import { Input } from "../../components/ui/Input";
import { Drawer } from "../../components/ui/Drawer";
import { EmptyState } from "../../layout/EmptyState";
import { useAuth } from "../../auth/AuthProvider";
import { ApiError } from "../../lib/apiClient";
import type { Incident, IncidentStatus } from "../../types/api";
import { useServices } from "../services/hooks";
import {
  useAddIncidentUpdate,
  useCreateIncident,
  useIncidents,
  useIncidentUpdates,
  useTransitionIncident,
} from "./hooks";

const activeStatusLabel: Record<Exclude<IncidentStatus, "resolved">, string> = {
  investigating: "Investigando",
  identified: "Identificado",
  monitoring: "Monitorando",
};

const transitionOptions: { value: IncidentStatus; label: string }[] = [
  { value: "identified", label: "Identificado" },
  { value: "monitoring", label: "Monitorando" },
  { value: "resolved", label: "Marcar como resolvido" },
];

function PlusIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
      <path d="M12 5v14M5 12h14" />
    </svg>
  );
}

function CheckCircleIcon() {
  return (
    <svg width="28" height="28" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <circle cx="12" cy="12" r="9" stroke="currentColor" strokeWidth="1.5" />
      <path d="M8 12l2.5 2.5L16 9" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  );
}

function ReloadIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path
        d="M20 12a8 8 0 10-2.7 6M20 6v6h-6"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}

function ReopenButton({ incident }: { incident: Incident }) {
  const transition = useTransitionIncident(incident.id);
  return (
    <Button
      variant="ghost"
      onClick={() => transition.mutate("investigating")}
      disabled={transition.isPending}
    >
      <ReloadIcon />
      Reabrir incidente
    </Button>
  );
}

function formatActiveTimestamp(iso: string): string {
  return new Date(iso).toLocaleString("pt-BR", { day: "2-digit", month: "short", hour: "2-digit", minute: "2-digit" });
}

function formatResolvedTimestamp(iso: string): string {
  return new Date(iso).toLocaleString("pt-BR", { day: "2-digit", month: "short", year: "numeric" });
}

function ActiveIncidentCard({
  incident,
  canManage,
  serviceName,
}: {
  incident: Incident;
  canManage: boolean;
  serviceName: (id: string) => string;
}) {
  const [expanded, setExpanded] = useState(false);
  // SPEC_DEVIATION: fixed page 1, no Pager for this per-incident timeline -
  // see the equivalent note in IncidentDetail.tsx.
  const { data: updatesPage } = useIncidentUpdates(incident.id, 1);
  const updates = updatesPage?.items;
  const addUpdate = useAddIncidentUpdate(incident.id);
  const transition = useTransitionIncident(incident.id);
  const [body, setBody] = useState("");

  async function handlePublish(e: FormEvent) {
    e.preventDefault();
    if (!body.trim()) return;
    await addUpdate.mutateAsync(body);
    setBody("");
  }

  return (
    <Card elevation="elev-sm" className="flex flex-col gap-2 p-4">
      <div className="flex items-start justify-between">
        <div className="flex items-center gap-2">
          <Tag variant="accent">
            {activeStatusLabel[incident.status as Exclude<IncidentStatus, "resolved">]}
          </Tag>
          <span className="text-[15px] font-medium text-text">{incident.title}</span>
        </div>
        <span className="text-xs text-neutral-400">{formatActiveTimestamp(incident.created_at)}</span>
      </div>

      <div className="flex flex-wrap gap-1">
        {incident.service_ids.map((id) => (
          <Tag key={id} variant="neutral">
            {serviceName(id)}
          </Tag>
        ))}
      </div>

      <button
        type="button"
        onClick={() => setExpanded((v) => !v)}
        className="w-fit cursor-pointer text-xs text-accent hover:underline"
      >
        Ver timeline ( {(updates ?? []).length} )
      </button>

      {expanded ? (
        <>
          <div className="h-px bg-divider" />
          <div className="flex flex-col gap-3">
            {(updates ?? []).map((update) => (
              <div key={update.id} className="flex gap-3">
                <div className="mt-1.5 h-1.5 w-1.5 shrink-0 rounded-full bg-accent" />
                <div className="flex flex-col gap-0.5">
                  <p className="text-sm text-text">{update.body}</p>
                  <p className="text-xs text-neutral-400">
                    {new Date(update.created_at).toLocaleString("pt-BR")}
                  </p>
                </div>
              </div>
            ))}
          </div>

          {canManage ? (
            <form onSubmit={handlePublish} className="flex items-center gap-2">
              <Input
                aria-label="Adicionar atualização"
                value={body}
                onChange={(e) => setBody(e.target.value)}
                placeholder="Adicionar atualização…"
              />
              <Button type="submit" variant="primary" disabled={addUpdate.isPending}>
                Publicar
              </Button>
            </form>
          ) : null}

          {canManage ? (
            <div className="flex flex-wrap gap-2">
              {transitionOptions.map((opt) => (
                <Button
                  key={opt.value}
                  variant="secondary"
                  disabled={transition.isPending || incident.status === opt.value}
                  onClick={() => transition.mutate(opt.value)}
                >
                  {opt.label}
                </Button>
              ))}
            </div>
          ) : null}
        </>
      ) : null}
    </Card>
  );
}

export function IncidentsPage() {
  const { hasRole } = useAuth();
  const canManage = hasRole(["owner", "operator"]);
  const [tab, setTab] = useState<"active" | "resolved">("active");
  // SPEC_DEVIATION: task T5 (IncidentsPage renders Pager) depends on T14
  // (Pager component), which is a later phase not yet built - this reads
  // fixed page 1 for now (25 incidents), matching what "all incidents"
  // meant before pagination for any installation with 25 or fewer. Real
  // page navigation (Pager wired to page state) is T5's job once T14 exists.
  const { data: incidentsPage, isLoading } = useIncidents(1);
  const incidents = incidentsPage?.items;
  // SPEC_DEVIATION: fixed page 1 for now - Pager UI for the services
  // dropdown/lookup is out of scope here; T14/T16 (Pager) is a later
  // phase not yet built. Mirrors the same deviation in ServicesSection.tsx.
  const { data: servicesPage } = useServices(1);
  const services = servicesPage?.items;
  const createIncident = useCreateIncident();

  const [dialogOpen, setDialogOpen] = useState(false);
  const [title, setTitle] = useState("");
  const [serviceIds, setServiceIds] = useState<string[]>([]);
  const [error, setError] = useState<string | null>(null);

  function toggleService(id: string) {
    setServiceIds((prev) => (prev.includes(id) ? prev.filter((s) => s !== id) : [...prev, id]));
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    try {
      await createIncident.mutateAsync({ title, service_ids: serviceIds });
      setTitle("");
      setServiceIds([]);
      setDialogOpen(false);
    } catch (err) {
      if (err instanceof ApiError) setError(err.message);
      else setError("Não foi possível criar o incidente.");
    }
  }

  const active = (incidents ?? []).filter((i) => i.status !== "resolved");
  const resolved = (incidents ?? []).filter((i) => i.status === "resolved");
  const list = tab === "active" ? active : resolved;

  function serviceName(id: string): string {
    return services?.find((s) => s.id === id)?.name ?? id;
  }

  return (
    <div className="mx-auto flex w-full max-w-[1280px] flex-col gap-6">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-text">Incidentes</h2>
          <p className="m-0 text-[13.5px] text-neutral-400">
            Acompanhe e comunique incidentes vinculados aos serviços monitorados.
          </p>
        </div>
        {canManage ? (
          <Button variant="primary" onClick={() => setDialogOpen(true)}>
            <PlusIcon />
            Novo incidente
          </Button>
        ) : null}
      </div>

      <Seg
        aria-label="Filtrar incidentes"
        options={[
          { value: "active", label: "Ativos" },
          { value: "resolved", label: "Resolvidos" },
        ]}
        value={tab}
        onChange={(v) => setTab(v as "active" | "resolved")}
      />

      {isLoading ? (
        <p className="text-neutral-400">Carregando…</p>
      ) : list.length === 0 && tab === "active" ? (
        <EmptyState
          title="Nenhum incidente ativo"
          description="Todos os serviços monitorados estão operando normalmente."
          action={<CheckCircleIcon />}
        />
      ) : list.length === 0 ? (
        <EmptyState title="Nenhum incidente resolvido ainda." />
      ) : tab === "active" ? (
        <div className="flex flex-col gap-3">
          {list.map((incident) => (
            <ActiveIncidentCard
              key={incident.id}
              incident={incident}
              canManage={canManage}
              serviceName={serviceName}
            />
          ))}
        </div>
      ) : (
        <div className="flex flex-col gap-3">
          {list.map((incident) => (
            <Card key={incident.id} elevation="elev-sm" className="flex flex-col gap-2 p-4">
              <div className="flex items-start justify-between">
                <div className="flex items-center gap-2">
                  <Tag variant="neutral">Resolvido</Tag>
                  <span className="text-[15px] font-medium text-text">{incident.title}</span>
                </div>
                <span className="text-xs text-neutral-400">
                  {formatResolvedTimestamp(incident.resolved_at ?? incident.created_at)}
                </span>
              </div>
              <div className="flex flex-wrap gap-1">
                {incident.service_ids.map((id) => (
                  <Tag key={id} variant="neutral">
                    {serviceName(id)}
                  </Tag>
                ))}
              </div>
              {canManage ? <ReopenButton incident={incident} /> : null}
            </Card>
          ))}
        </div>
      )}

      <Drawer
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        title="Criar incidente"
        description="Descreva o incidente e vincule os serviços afetados."
        footer={
          <div className="flex gap-2">
            <Button type="submit" form="create-incident-form" variant="primary" disabled={createIncident.isPending}>
              Criar
            </Button>
            <Button type="button" variant="secondary" onClick={() => setDialogOpen(false)}>
              Cancelar
            </Button>
          </div>
        }
      >
        <form id="create-incident-form" onSubmit={handleSubmit} className="flex flex-col gap-3">
          <Field label="Título" value={title} onChange={(e) => setTitle(e.target.value)} required />
          <div className="flex flex-col gap-1">
            <span className="text-sm font-medium text-text">Serviços afetados</span>
            <div className="flex flex-wrap gap-2">
              {(services ?? []).map((s) => {
                const isActive = serviceIds.includes(s.id);
                return (
                  <button key={s.id} type="button" onClick={() => toggleService(s.id)} className="cursor-pointer">
                    <Tag variant={isActive ? "accent" : "accent-outline"}>{s.name}</Tag>
                  </button>
                );
              })}
            </div>
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
