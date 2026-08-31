import { useState, type FormEvent } from "react";
import { Button } from "../../components/ui/Button";
import { Field } from "../../components/ui/Field";
import { Card } from "../../components/ui/Card";
import { Dialog } from "../../components/ui/Dialog";
import { Pager } from "../../components/ui/Pager";
import { Tooltip } from "../../components/ui/Tooltip";
import { useAuth } from "../../auth/AuthProvider";
import { ApiError } from "../../lib/apiClient";
import type { Domain } from "../../types/api";
import { useCreateDomain, useDeleteDomain, useDomains } from "./hooks";

function formatTimestamp(iso: string): string {
  return new Date(iso).toLocaleString("pt-BR");
}

function GlobeIcon() {
  return (
    <svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <circle cx="12" cy="12" r="9" />
      <path d="M3 12h18M12 3a14 14 0 0 1 0 18M12 3a14 14 0 0 0 0 18" />
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

/** Tabela + form de domínios. Compartilhada entre `DomainsStatusPagesPage` (handoff mostra as duas seções na mesma tela) e `DomainsPage` (rota própria, mesmo padrão de `ServicesSection`). */
export function DomainsSection() {
  const { hasRole } = useAuth();
  const canManage = hasRole(["owner", "operator"]);
  const [page, setPage] = useState(1);
  const { data: domainsPage, isLoading } = useDomains(page);
  const domains = domainsPage?.items;
  const totalPages = Math.max(1, Math.ceil((domainsPage?.total ?? 0) / (domainsPage?.page_size ?? 20)));
  const createDomain = useCreateDomain();

  const [formOpen, setFormOpen] = useState(false);
  const [hostname, setHostname] = useState("");
  const [error, setError] = useState<string | null>(null);

  const deleteDomain = useDeleteDomain();
  const [removeTarget, setRemoveTarget] = useState<Domain | null>(null);
  const [removeError, setRemoveError] = useState<string | null>(null);

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

  async function confirmRemove() {
    if (!removeTarget) return;
    setRemoveError(null);
    try {
      await deleteDomain.mutateAsync(removeTarget.id);
      setRemoveTarget(null);
    } catch (err) {
      if (err instanceof ApiError) setRemoveError(err.message);
      else setRemoveError("Não foi possível excluir o domínio.");
    }
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <h4 className="text-text">Domínios cadastrados</h4>
        {canManage ? (
          <Button variant="primary" onClick={() => setFormOpen((v) => !v)}>
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

      <div>
        {isLoading ? (
          <p className="text-neutral-400">Carregando…</p>
        ) : (
          <>
            <Card elevation="elev-sm" className="divide-y divide-divider overflow-hidden">
              {(domains ?? []).length === 0 ? (
                <p className="px-4 py-6 text-center text-neutral-400">Nenhum domínio cadastrado.</p>
              ) : (
                (domains ?? []).map((d) => (
                  <div key={d.id} data-testid="domain-row" className="flex items-center gap-3 px-4 py-3.5">
                    <div className="grid h-9 w-9 flex-none place-items-center rounded-[10px] bg-neutral-800 text-neutral-300">
                      <GlobeIcon />
                    </div>
                    <div className="flex-1 text-[15px] font-medium text-text">{d.hostname}</div>
                    <div className="text-right text-xs text-neutral-400">
                      <div>Cadastrado em</div>
                      <div className="mt-0.5 text-[13px] text-text">{formatTimestamp(d.created_at)}</div>
                    </div>
                    {canManage ? (
                      <Tooltip label="Excluir">
                        <Button
                          variant="ghost"
                          aria-label="Excluir"
                          className="text-neutral-400 hover:text-critical"
                          onClick={() => {
                            setRemoveError(null);
                            setRemoveTarget(d);
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
        title="Excluir domínio"
        description={
          removeTarget
            ? `Excluir o domínio ${removeTarget.hostname}? Esta ação não pode ser desfeita.`
            : undefined
        }
        footer={
          <>
            <Button variant="secondary" onClick={() => setRemoveTarget(null)}>
              Cancelar
            </Button>
            <Button variant="primary" onClick={confirmRemove} disabled={deleteDomain.isPending}>
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
    </div>
  );
}
