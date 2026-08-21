import { useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import { Seg } from "../../components/ui/Seg";
import { Card } from "../../components/ui/Card";
import { Tag } from "../../components/ui/Tag";
import { Button } from "../../components/ui/Button";
import { Field } from "../../components/ui/Field";
import { Dialog } from "../../components/ui/Dialog";
import { EmptyState } from "../../layout/EmptyState";
import { useAuth } from "../../auth/AuthProvider";
import { ApiError } from "../../lib/apiClient";
import type { Incident, IncidentStatus } from "../../types/api";
import { useServices } from "../services/hooks";
import { useCreateIncident, useIncidents, useTransitionIncident } from "./hooks";

const activeStatusLabel: Record<Exclude<IncidentStatus, "resolved">, string> = {
  investigating: "Investigando",
  identified: "Identificado",
  monitoring: "Monitorando",
};

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

export function IncidentsPage() {
  const { hasRole } = useAuth();
  const canManage = hasRole(["owner", "operator"]);
  const [tab, setTab] = useState<"active" | "resolved">("active");
  const { data: incidents, isLoading } = useIncidents();
  const { data: services } = useServices();
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
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <h3 className="text-text">Incidentes</h3>
        {canManage ? (
          <Button variant="primary" onClick={() => setDialogOpen(true)}>
            Criar incidente
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
      ) : (
        <div className="flex flex-col gap-3">
          {list.map((incident) => (
            <Card key={incident.id} elevation="elev-sm" className="flex flex-col gap-2 p-4">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  {incident.status === "resolved" ? (
                    <Tag variant="neutral">Resolvido</Tag>
                  ) : (
                    <Tag variant="accent">
                      {activeStatusLabel[incident.status as Exclude<IncidentStatus, "resolved">]}
                    </Tag>
                  )}
                  <Link to={`/incidents/${incident.id}`} className="text-text hover:underline">
                    {incident.title}
                  </Link>
                </div>
                {incident.status === "resolved" && canManage ? (
                  <ReopenButton incident={incident} />
                ) : null}
              </div>
              <div className="flex flex-wrap gap-1">
                {incident.service_ids.map((id) => (
                  <Tag key={id} variant="neutral-outline">
                    {serviceName(id)}
                  </Tag>
                ))}
              </div>
              <p className="text-xs text-neutral-400">
                {incident.status === "resolved"
                  ? `Resolvido em ${new Date(incident.resolved_at ?? incident.created_at).toLocaleString("pt-BR")}`
                  : `Criado em ${new Date(incident.created_at).toLocaleString("pt-BR")}`}
              </p>
            </Card>
          ))}
        </div>
      )}

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen} title="Criar incidente">
        <form onSubmit={handleSubmit} className="flex flex-col gap-3">
          <Field label="Título" value={title} onChange={(e) => setTitle(e.target.value)} required />
          <div className="flex flex-col gap-1">
            <span className="text-sm font-medium text-text">Serviços afetados</span>
            <div className="flex flex-wrap gap-2">
              {(services ?? []).map((s) => {
                const isActive = serviceIds.includes(s.id);
                return (
                  <button key={s.id} type="button" onClick={() => toggleService(s.id)}>
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
          <div className="flex gap-2">
            <Button type="submit" variant="primary" disabled={createIncident.isPending}>
              Criar
            </Button>
            <Button type="button" variant="secondary" onClick={() => setDialogOpen(false)}>
              Cancelar
            </Button>
          </div>
        </form>
      </Dialog>
    </div>
  );
}
