import { useState, type FormEvent } from "react";
import { Table, type TableColumn } from "../../components/ui/Table";
import { Button } from "../../components/ui/Button";
import { Field } from "../../components/ui/Field";
import { Card } from "../../components/ui/Card";
import { useAuth } from "../../auth/AuthProvider";
import { ApiError } from "../../lib/apiClient";
import type { Domain } from "../../types/api";
import { useCreateDomain, useDomains } from "./hooks";

function formatTimestamp(iso: string): string {
  return new Date(iso).toLocaleString("pt-BR");
}

/** Tabela + form de domínios. Compartilhada entre `DomainsStatusPagesPage` (handoff mostra as duas seções na mesma tela) e `DomainsPage` (rota própria, mesmo padrão de `ServicesSection`). */
export function DomainsSection() {
  const { hasRole } = useAuth();
  const canManage = hasRole(["owner", "operator"]);
  const { data: domains, isLoading } = useDomains();
  const createDomain = useCreateDomain();

  const [formOpen, setFormOpen] = useState(false);
  const [hostname, setHostname] = useState("");
  const [error, setError] = useState<string | null>(null);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    try {
      await createDomain.mutateAsync({ hostname });
      setHostname("");
      setFormOpen(false);
    } catch (err) {
      if (err instanceof ApiError) setError(err.message);
      else setError("Não foi possível cadastrar o domínio.");
    }
  }

  const columns: TableColumn<Domain>[] = [
    { key: "hostname", header: "Hostname", render: (d) => d.hostname },
    { key: "created_at", header: "Cadastrado em", render: (d) => formatTimestamp(d.created_at) },
  ];

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <h4 className="text-text">Domínios cadastrados</h4>
        {canManage ? (
          <Button variant="secondary" onClick={() => setFormOpen((v) => !v)}>
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
              <path d="M12 5v14M5 12h14" />
            </svg>
            Adicionar domínio
          </Button>
        ) : null}
      </div>

      {formOpen && canManage ? (
        <Card elevation="elev-sm" className="max-w-md p-5">
          <form onSubmit={handleSubmit} className="flex flex-col gap-3">
            <Field
              label="Hostname"
              value={hostname}
              onChange={(e) => setHostname(e.target.value)}
              placeholder="status.suaempresa.com"
              error={error ?? undefined}
              required
            />
            <div className="flex gap-2">
              <Button type="submit" variant="primary" disabled={createDomain.isPending}>
                Salvar
              </Button>
              <Button type="button" variant="secondary" onClick={() => setFormOpen(false)}>
                Cancelar
              </Button>
            </div>
          </form>
        </Card>
      ) : null}

      <div className="mt-2">
        {isLoading ? (
          <p className="text-neutral-400">Carregando…</p>
        ) : (
          <Table
            columns={columns}
            rows={domains ?? []}
            rowKey={(d) => d.id}
            emptyMessage="Nenhum domínio cadastrado."
          />
        )}
      </div>
    </div>
  );
}
