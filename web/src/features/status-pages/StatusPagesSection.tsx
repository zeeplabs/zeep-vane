import { useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import { Card } from "../../components/ui/Card";
import { Dialog } from "../../components/ui/Dialog";
import { Drawer } from "../../components/ui/Drawer";
import { Pager } from "../../components/ui/Pager";
import { Button } from "../../components/ui/Button";
import { Field } from "../../components/ui/Field";
import { Tag } from "../../components/ui/Tag";
import { Tooltip } from "../../components/ui/Tooltip";
import { useAuth } from "../../auth/AuthProvider";
import { ApiError } from "../../lib/apiClient";
import type { StatusPage } from "../../types/api";
import { useDomains } from "../domains/hooks";
import { useServices } from "../services/hooks";
import { useCreateStatusPage, useDeleteStatusPage, useStatusPages } from "./hooks";

function LayoutIcon() {
  return (
    <svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <rect x="3" y="4" width="18" height="16" rx="2" />
      <path d="M3 9h18" />
    </svg>
  );
}

function TrashIcon() {
  return (
    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13" />
    </svg>
  );
}

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
  const [page, setPage] = useState(1);
  const { data: statusPages, isLoading } = useStatusPages(page);
  const pages = statusPages?.items;
  const totalPages = Math.max(1, Math.ceil((statusPages?.total ?? 0) / (statusPages?.page_size ?? 20)));
  // SPEC_DEVIATION: fixed page 1 for now - Pager UI for the domains
  // dropdown is out of scope here (this reads domains only to resolve a
  // hostname/build a select list); T14/T16 (Pager) is a later phase not
  // yet built. Mirrors the same deviation in DomainsSection.tsx.
  const { data: domainsPage } = useDomains(1);
  const domains = domainsPage?.items;
  // SPEC_DEVIATION: fixed page 1 for now - Pager UI for the services
  // dropdown/lookup is out of scope here; T14/T16 (Pager) is a later
  // phase not yet built. Mirrors the same deviation in ServicesSection.tsx.
  const { data: servicesPage } = useServices(1);
  const services = servicesPage?.items;
  const createStatusPage = useCreateStatusPage();
  const deleteStatusPage = useDeleteStatusPage();

  const [dialogOpen, setDialogOpen] = useState(false);
  const [name, setName] = useState("");
  const [serviceIds, setServiceIds] = useState<string[]>([]);
  const [error, setError] = useState<string | null>(null);

  const [removeTarget, setRemoveTarget] = useState<StatusPage | null>(null);
  const [removeError, setRemoveError] = useState<string | null>(null);

  async function confirmRemove() {
    if (!removeTarget) return;
    setRemoveError(null);
    try {
      await deleteStatusPage.mutateAsync(removeTarget.id);
      setRemoveTarget(null);
    } catch (err) {
      if (err instanceof ApiError) setRemoveError(err.message);
      else setRemoveError("Não foi possível excluir a status page.");
    }
  }

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

  // SPD-14: published/tls_failed keep today's labels unchanged, checked
  // first since they're the terminal states - independent of domain_id (a
  // real published/tls_failed page always has a domain attached;
  // publicUrl()'s own null-safety guard still protects the defensive,
  // never-real, published-without-domain shape).
  function stateBlock(p: StatusPage) {
    if (p.state === "published") {
      const url = publicUrl(p, hostnameFor(p));
      return (
        <div className="flex flex-col items-end gap-0.5">
          {url ? (
            <a href={url} target="_blank" rel="noreferrer" className="text-xs text-accent hover:underline">
              {url}
            </a>
          ) : null}
          <Tag variant="success">Publicada</Tag>
        </div>
      );
    }
    if (p.state === "tls_failed") {
      return (
        <div className="flex flex-col items-end gap-0.5">
          <Tag variant="critical">Falha</Tag>
          <span className="text-xs text-neutral-400">{p.tls_last_error}</span>
        </div>
      );
    }
    // SPD-12: sem domínio nenhum anexado ainda - distinto do "aguardando
    // DNS/certificado" abaixo, com uma ação pra sair desse estado. Mesma
    // lógica de StatusPageDetail.tsx, aplicada na lista.
    if (p.domain_id === null) {
      return (
        <div className="flex flex-col items-end gap-1">
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
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <h4 className="text-text">Status pages</h4>
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
            Criar status page
          </Button>
        ) : null}
      </div>

      <div>
        {isLoading ? (
          <p className="text-neutral-400">Carregando…</p>
        ) : (
          <>
            <Card elevation="elev-sm" className="divide-y divide-divider overflow-hidden">
              {(pages ?? []).length === 0 ? (
                <p className="px-4 py-6 text-center text-neutral-400">Nenhuma status page criada.</p>
              ) : (
                (pages ?? []).map((p) => (
                  <div key={p.id} data-testid="status-page-row" className="flex items-center gap-3 px-4 py-3.5">
                    <div className="grid h-9 w-9 flex-none place-items-center rounded-[10px] bg-neutral-800 text-neutral-300">
                      <LayoutIcon />
                    </div>
                    <div className="flex-1">
                      <Link to={`/status-pages/${p.id}`} className="text-[15px] font-medium text-text hover:underline">
                        {p.name}
                      </Link>
                      <div className="mt-0.5 text-xs text-neutral-400">{p.subdomain ?? "—"}</div>
                    </div>
                    {stateBlock(p)}
                    {canManage ? (
                      <Tooltip label="Excluir">
                        <Button
                          variant="ghost"
                          aria-label="Excluir"
                          className="text-neutral-400 hover:text-critical"
                          onClick={() => {
                            setRemoveError(null);
                            setRemoveTarget(p);
                          }}
                        >
                          <TrashIcon />
                        </Button>
                      </Tooltip>
                    ) : null}
                  </div>
                ))
              )}
            </Card>
            <Pager page={page} totalPages={totalPages} onChange={setPage} />
          </>
        )}
      </div>

      <Dialog
        open={removeTarget !== null}
        onOpenChange={(open) => {
          if (!open) setRemoveTarget(null);
        }}
        title="Excluir status page"
        description={
          removeTarget ? `Excluir a status page "${removeTarget.name}"? Esta ação não pode ser desfeita.` : undefined
        }
        footer={
          <>
            <Button variant="secondary" onClick={() => setRemoveTarget(null)}>
              Cancelar
            </Button>
            <Button variant="primary" onClick={confirmRemove} disabled={deleteStatusPage.isPending}>
              Excluir
            </Button>
          </>
        }
      >
        {removeError ? (
          <p role="alert" className="text-xs text-critical">
            {removeError}
          </p>
        ) : null}
      </Dialog>

      <Drawer
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        title="Criar status page"
        description="Vincule os serviços que essa status page vai exibir. O domínio é anexado depois, numa tela dedicada."
        footer={
          <>
            <Button type="button" variant="secondary" onClick={() => setDialogOpen(false)}>
              Cancelar
            </Button>
            <Button type="submit" form="create-status-page-form" variant="primary" disabled={createStatusPage.isPending}>
              Criar
            </Button>
          </>
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
