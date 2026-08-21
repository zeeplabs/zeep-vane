import { useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import { Table, type TableColumn } from "../../components/ui/Table";
import { Drawer } from "../../components/ui/Drawer";
import { Button } from "../../components/ui/Button";
import { Field } from "../../components/ui/Field";
import { Tag } from "../../components/ui/Tag";
import { useAuth } from "../../auth/AuthProvider";
import { ApiError } from "../../lib/apiClient";
import type { StatusPage } from "../../types/api";
import { useDomains } from "../domains/hooks";
import { useServices } from "../services/hooks";
import { useCreateStatusPage, useStatusPages } from "./hooks";

function publicUrl(page: StatusPage, hostname: string | undefined): string {
  return `https://${page.subdomain}.${hostname ?? "?"}`;
}

/** Tabela + dialog de status pages. Compartilhada entre `DomainsStatusPagesPage` (handoff mostra as duas seções na mesma tela) e `StatusPagesPage` (rota própria, mesmo padrão de `ServicesSection`). */
export function StatusPagesSection() {
  const { hasRole } = useAuth();
  const canManage = hasRole(["owner", "operator"]);
  const { data: pages, isLoading } = useStatusPages();
  const { data: domains } = useDomains();
  const { data: services } = useServices();
  const createStatusPage = useCreateStatusPage();

  const [dialogOpen, setDialogOpen] = useState(false);
  const [name, setName] = useState("");
  const [subdomain, setSubdomain] = useState("");
  const [domainId, setDomainId] = useState("");
  const [serviceIds, setServiceIds] = useState<string[]>([]);
  const [error, setError] = useState<string | null>(null);

  function toggleService(id: string) {
    setServiceIds((prev) => (prev.includes(id) ? prev.filter((s) => s !== id) : [...prev, id]));
  }

  function resetForm() {
    setName("");
    setSubdomain("");
    setDomainId(domains?.[0]?.id ?? "");
    setServiceIds([]);
    setError(null);
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    try {
      await createStatusPage.mutateAsync({ name, subdomain, domain_id: domainId, service_ids: serviceIds });
      resetForm();
      setDialogOpen(false);
    } catch (err) {
      if (err instanceof ApiError) setError(err.message);
      else setError("Não foi possível criar a status page.");
    }
  }

  function hostnameFor(page: StatusPage): string | undefined {
    return domains?.find((d) => d.id === page.domain_id)?.hostname;
  }

  const columns: TableColumn<StatusPage>[] = [
    { key: "name", header: "Nome", render: (p) => <Link to={`/status-pages/${p.id}`}>{p.name}</Link> },
    { key: "subdomain", header: "Subdomínio", render: (p) => p.subdomain },
    {
      key: "state",
      header: "Estado",
      render: (p) => {
        if (p.state === "draft") {
          return (
            <Tag variant="accent" data-testid="pulsing-tag" className="animate-pulse">
              Emitindo certificado
            </Tag>
          );
        }
        if (p.state === "published") {
          return (
            <div className="flex flex-col gap-0.5">
              <Tag variant="success">Publicada</Tag>
              <a
                href={publicUrl(p, hostnameFor(p))}
                target="_blank"
                rel="noreferrer"
                className="text-xs text-accent hover:underline"
              >
                {publicUrl(p, hostnameFor(p))}
              </a>
            </div>
          );
        }
        return (
          <div className="flex flex-col gap-0.5">
            <Tag variant="critical">Falha</Tag>
            <span className="text-xs text-neutral-400">{p.tls_last_error}</span>
          </div>
        );
      },
    },
  ];

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <h4 className="text-text">Status pages</h4>
        {canManage ? (
          <Button
            variant="secondary"
            onClick={() => {
              resetForm();
              setDialogOpen(true);
            }}
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
              <path d="M12 5v14M5 12h14" />
            </svg>
            Criar status page
          </Button>
        ) : null}
      </div>

      <div className="mt-2">
        {isLoading ? (
          <p className="text-neutral-400">Carregando…</p>
        ) : (
          <Table
            columns={columns}
            rows={pages ?? []}
            rowKey={(p) => p.id}
            emptyMessage="Nenhuma status page criada."
          />
        )}
      </div>

      <Drawer
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        title="Criar status page"
        description="Escolha o domínio e vincule os serviços que essa status page vai exibir."
        footer={
          <div className="flex gap-2">
            <Button type="submit" form="create-status-page-form" variant="primary" disabled={createStatusPage.isPending}>
              Criar
            </Button>
            <Button type="button" variant="secondary" onClick={() => setDialogOpen(false)}>
              Cancelar
            </Button>
          </div>
        }
      >
        <form id="create-status-page-form" onSubmit={handleSubmit} className="flex flex-col gap-3">
          <Field label="Nome" value={name} onChange={(e) => setName(e.target.value)} required />
          <Field
            label="Subdomínio"
            value={subdomain}
            onChange={(e) => setSubdomain(e.target.value)}
            required
          />
          <div className="flex flex-col gap-1">
            <label htmlFor="domain-picker" className="text-sm font-medium text-text">
              Domínio
            </label>
            <select
              id="domain-picker"
              value={domainId}
              onChange={(e) => setDomainId(e.target.value)}
              className="min-h-9 rounded-md border border-divider bg-surface px-3 text-sm text-text"
              required
            >
              <option value="" disabled>
                Selecione um domínio
              </option>
              {(domains ?? []).map((d) => (
                <option key={d.id} value={d.id}>
                  {d.hostname}
                </option>
              ))}
            </select>
          </div>
          <div className="flex flex-col gap-1">
            <span className="text-sm font-medium text-text">Serviços</span>
            <div className="flex flex-wrap gap-2">
              {(services ?? []).map((s) => {
                const active = serviceIds.includes(s.id);
                return (
                  <button
                    key={s.id}
                    type="button"
                    onClick={() => toggleService(s.id)}
                    aria-pressed={active}
                    className="cursor-pointer"
                  >
                    <Tag variant={active ? "accent" : "accent-outline"}>{s.name}</Tag>
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
