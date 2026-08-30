import { useState, type FormEvent } from "react";
import { Table, type TableColumn } from "../../components/ui/Table";
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
  not_configured: "neutral",
};

function formatTimestamp(iso: string): string {
  return new Date(iso).toLocaleString("pt-BR");
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

  const columns: TableColumn<Service>[] = [
    { key: "name", header: "Serviço", render: (s) => s.name },
    { key: "slo", header: "SLO vinculado", render: (s) => s.slo_name ?? "—" },
    {
      key: "status",
      header: "Status",
      render: (s) => (
        <Tag variant={statusVariant[s.current_status]}>{statusLabel[s.current_status]}</Tag>
      ),
    },
    {
      key: "last_change",
      header: "Última mudança",
      render: (s) => (s.current_status === "not_configured" ? "—" : formatTimestamp(s.last_status_change_at)),
    },
  ];

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <h4 className="text-text">Serviços monitorados</h4>
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

      <div className="mt-2">
        {isLoading ? (
          <p className="text-neutral-400">Carregando…</p>
        ) : (
          <>
            <Table
              columns={columns}
              rows={services ?? []}
              rowKey={(s) => s.id}
              emptyMessage="Nenhum serviço cadastrado."
            />
            <Pager page={page} totalPages={totalPages} onChange={setPage} />
          </>
        )}
      </div>

      <Dialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        title="Vincular serviço"
        description="Associe um serviço a um SLO existente no Datadog."
      >
        <form onSubmit={handleSubmit} className="flex flex-col gap-3">
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
          <div className="flex gap-2">
            <Button type="submit" variant="primary" disabled={createService.isPending}>
              Salvar
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
