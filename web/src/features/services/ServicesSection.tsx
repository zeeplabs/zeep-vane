import { useState, type FormEvent } from "react";
import { Card } from "../../components/ui/Card";
import { Dialog } from "../../components/ui/Dialog";
import { Button } from "../../components/ui/Button";
import { Field } from "../../components/ui/Field";
import { Pager } from "../../components/ui/Pager";
import { Tag, type TagVariant } from "../../components/ui/Tag";
import { useAuth } from "../../auth/AuthProvider";
import { ApiError } from "../../lib/apiClient";
import type { Service, ServiceStatus } from "../../types/api";
import { useSLOSearch } from "../integrations/hooks";
import { useCreateService, useServices } from "./hooks";

const statusLabel: Record<ServiceStatus, string> = {
  operational: "Operacional",
  degraded: "Degradado",
  outage: "Inoperante",
  not_configured: "Não configurado",
};

const statusVariant: Record<ServiceStatus, TagVariant> = {
  operational: "success",
  degraded: "warning",
  outage: "critical",
  not_configured: "neutral-outline",
};

const statusDotColor: Record<ServiceStatus, string> = {
  operational: "var(--color-success)",
  degraded: "var(--color-warning)",
  outage: "var(--color-critical)",
  not_configured: "var(--color-neutral-600)",
};

function formatTimestamp(iso: string): string {
  return new Date(iso).toLocaleString("pt-BR");
}

function ClockIcon() {
  return (
    <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <circle cx="12" cy="12" r="9" />
      <path d="M12 7v5l3.5 2" />
    </svg>
  );
}

/** Tabela + dialog de vínculo de serviço a SLO. Compartilhada entre `IntegrationsPage` (handoff mostra as duas seções na mesma tela) e `ServicesPage` (rota própria, decisão registrada em design.md). */
export function ServicesSection() {
  const { hasRole } = useAuth();
  const canManage = hasRole(["owner", "operator"]);
  const [page, setPage] = useState(1);
  const { data: servicesPage, isLoading } = useServices(page);
  const services = servicesPage?.items;
  const totalPages = Math.max(1, Math.ceil((servicesPage?.total ?? 0) / (servicesPage?.page_size ?? 20)));
  const createService = useCreateService();

  const [dialogOpen, setDialogOpen] = useState(false);
  const [name, setName] = useState("");
  const [query, setQuery] = useState("");
  const [selectedSlo, setSelectedSlo] = useState<{ id: string; name: string } | null>(null);
  const [error, setError] = useState<string | null>(null);

  const sloSearch = useSLOSearch(query);

  function resetForm() {
    setName("");
    setQuery("");
    setSelectedSlo(null);
    setError(null);
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    if (!selectedSlo) {
      // The real backend requires slo_id on creation (SPEC_DEVIATION, I15:
      // the earlier mock allowed a service with no SLO at all) - validated
      // client-side so the admin gets an immediate, specific message
      // instead of a generic 422 from the API.
      setError("Selecione um SLO da lista antes de salvar.");
      return;
    }
    try {
      await createService.mutateAsync({ name, slo_id: selectedSlo.id });
      resetForm();
      setDialogOpen(false);
    } catch (err) {
      if (err instanceof ApiError) setError(err.message);
      else setError("Não foi possível vincular o serviço.");
    }
  }

  function serviceLastChange(s: Service): string {
    return s.current_status === "not_configured" ? "—" : formatTimestamp(s.last_status_change_at);
  }

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between">
        <h2 className="text-text">Serviços monitorados</h2>
        {canManage ? (
          <Button
            variant="primary"
            onClick={() => {
              resetForm();
              setDialogOpen(true);
            }}
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
              <path d="M12 5v14M5 12h14" />
            </svg>
            Vincular serviço
          </Button>
        ) : null}
      </div>

      <div>
        {isLoading ? (
          <p className="text-neutral-400">Carregando…</p>
        ) : (
          <>
            <Card elevation="elev-sm" className="divide-y divide-divider overflow-hidden">
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

      <Dialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        title="Vincular serviço"
        description="Associe um serviço a um SLO existente no Datadog."
        footer={
          <>
            <Button type="button" variant="secondary" onClick={() => setDialogOpen(false)}>
              Cancelar
            </Button>
            <Button type="submit" form="link-service-form" variant="primary" disabled={createService.isPending}>
              Salvar
            </Button>
          </>
        }
      >
        <form id="link-service-form" onSubmit={handleSubmit} className="flex flex-col gap-3">
          <Field
            label="Nome do serviço"
            value={name}
            onChange={(e) => setName(e.target.value)}
            required
          />
          <Field
            label="Buscar SLO"
            value={query}
            onChange={(e) => {
              setQuery(e.target.value);
              setSelectedSlo(null);
            }}
            placeholder="Digite o nome do SLO"
          />
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
          {error ? (
            <p role="alert" className="text-xs text-critical">
              {error}
            </p>
          ) : null}
        </form>
      </Dialog>
    </div>
  );
}
