import { useState, type FormEvent } from "react";
import { Table, type TableColumn } from "../../components/ui/Table";
import { Button } from "../../components/ui/Button";
import { Field } from "../../components/ui/Field";
import { Card } from "../../components/ui/Card";
import { useAuth } from "../../auth/AuthProvider";
import { ApiError } from "../../lib/apiClient";
import type { Domain } from "../../lib/mockData";
import { useCreateDomain, useDomains } from "./hooks";

function formatTimestamp(iso: string): string {
  return new Date(iso).toLocaleString("pt-BR");
}

export function DomainsPage() {
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
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <h3 className="text-text">Domínios</h3>
        {canManage ? (
          <Button variant="primary" onClick={() => setFormOpen((v) => !v)}>
            Cadastrar domínio
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
  );
}
