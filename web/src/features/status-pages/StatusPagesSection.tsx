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

// publicUrl only composes a URL once both domain_id/subdomain are set
// (SPD-01) - a domain-less page is always in "draft" state (attaching a
// domain is the only way to leave that state), so this null-safe guard
// is defensive: it never renders a broken "https://null.undefined".
function publicUrl(page: StatusPage, hostname: string | undefined): string | null {
  if (!page.domain_id || !page.subdomain) return null;
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
  const [serviceIds, setServiceIds] = useState<string[]>([]);
  const [error, setError] = useState<string | null>(null);

  function toggleService(id: string) {
    setServiceIds((prev) => (prev.includes(id) ? prev.filter((s) => s !== id) : [...prev, id]));
  }

  function resetForm() {
    setName("");
    setServiceIds([]);
    setError(null);
  }

  // SPD-01: creates a domain-less status page - the domain is attached
  // later, from a dedicated screen (AttachDomainDrawer, T14/T15).
  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    try {
      await createStatusPage.mutateAsync({ name, service_ids: serviceIds });
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
    { key: "subdomain", header: "Subdomínio", render: (p) => p.subdomain ?? "—" },
    {
      key: "state",
      header: "Estado",
      render: (p) => {
        // SPD-14: published/tls_failed keep today's labels unchanged,
        // checked first since they're the terminal states - independent of
        // domain_id (a real published/tls_failed page always has a domain
        // attached; publicUrl()'s own null-safety guard still protects the
        // defensive, never-real, published-without-domain shape).
        if (p.state === "published") {
          const url = publicUrl(p, hostnameFor(p));
          return (
            <div className="flex flex-col gap-0.5">
              <Tag variant="success">Publicada</Tag>
              {url ? (
                <a href={url} target="_blank" rel="noreferrer" className="text-xs text-accent hover:underline">
                  {url}
                </a>
              ) : null}
            </div>
          );
        }
        if (p.state === "tls_failed") {
          return (
            <div className="flex flex-col gap-0.5">
              <Tag variant="critical">Falha</Tag>
              <span className="text-xs text-neutral-400">{p.tls_last_error}</span>
            </div>
          );
        }
        // SPD-12: sem domínio nenhum anexado ainda - distinto do "aguardando
        // DNS/certificado" abaixo, com uma ação pra sair desse estado.
        // Mesma lógica de StatusPageDetail.tsx, aplicada na lista.
        if (p.domain_id === null) {
          return (
            <div className="flex flex-col gap-1">
              <Tag variant="accent-outline" className="w-fit">
                Sem domínio configurado
              </Tag>
              <Link to={`/status-pages/${p.id}`} className="text-xs text-accent hover:underline">
                Anexar domínio
              </Link>
            </div>
          );
        }
        // SPD-13: domínio anexado, mas o certificado ainda não foi emitido -
        // substitui o antigo texto ambíguo "Emitindo certificado", que não
        // distinguia esse caso do de "sem domínio" acima.
        return (
          <Tag variant="accent" data-testid="pulsing-tag" className="animate-pulse">
            Aguardando validação de DNS/certificado
          </Tag>
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
        description="Vincule os serviços que essa status page vai exibir. O domínio é anexado depois, numa tela dedicada."
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
