import { useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { MdOutlinePublic, MdOutlineDeleteOutline, MdOutlineAdd } from "react-icons/md";
import { Button } from "../../components/ui/Button";
import { Field } from "../../components/ui/Field";
import { Card } from "../../components/ui/Card";
import { Dialog } from "../../components/ui/Dialog";
import { Pager } from "../../components/ui/Pager";
import { Tooltip } from "../../components/ui/Tooltip";
import { Skeleton } from "../../components/ui/Skeleton";
import { useAuth } from "../../auth/AuthProvider";
import { ApiError } from "../../lib/apiClient";
import { formatDateTime } from "../../lib/formatDate";
import type { Domain } from "../../types/api";
import { useCreateDomain, useDeleteDomain, useDomains } from "./hooks";

function GlobeIcon() {
  return <MdOutlinePublic size={17} aria-hidden="true" />;
}

function TrashIcon() {
  return <MdOutlineDeleteOutline size={15} aria-hidden="true" />;
}

/** Tabela + form de domínios. Compartilhada entre `DomainsStatusPagesPage` (handoff mostra as duas seções na mesma tela) e `DomainsPage` (rota própria, mesmo padrão de `ServicesSection`). */
export function DomainsSection() {
  const { t, i18n } = useTranslation();
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
      else setError(t("domains.form.error"));
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
      else setRemoveError(t("domains.section.deleteError"));
    }
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <h4 className="text-text">{t("domains.section.title")}</h4>
        {canManage ? (
          <Button variant="primary" onClick={() => setFormOpen((v) => !v)}>
            <MdOutlineAdd size={14} aria-hidden="true" />
            {t("domains.addButton")}
          </Button>
        ) : null}
      </div>

      {formOpen && canManage ? (
        <Card elevation="none" className="border border-divider max-w-md p-5">
          <form onSubmit={handleSubmit} className="flex flex-col gap-3">
            <Field
              label={t("domains.form.hostnameLabel")}
              value={hostname}
              onChange={(e) => setHostname(e.target.value)}
              placeholder={t("domains.form.hostnamePlaceholder")}
              error={error ?? undefined}
              required
            />
            <div className="flex gap-2">
              <Button type="submit" variant="primary" disabled={createDomain.isPending}>
                {t("domains.form.saveButton")}
              </Button>
              <Button type="button" variant="secondary" onClick={() => setFormOpen(false)}>
                {t("common.cancel")}
              </Button>
            </div>
          </form>
        </Card>
      ) : null}

      <div>
        {isLoading ? (
          <div aria-busy="true" className="overflow-hidden rounded-md border border-divider divide-y divide-divider">
            <span className="sr-only">{t("domains.loading")}</span>
            {Array.from({ length: 4 }).map((_, i) => (
              <div key={i} className="flex items-center gap-3 px-4 py-3.5">
                <Skeleton width={36} height={36} radius={10} />
                <Skeleton height={14} className="flex-1" />
                <Skeleton width={90} height={28} />
              </div>
            ))}
          </div>
        ) : (
          <>
            <Card elevation="none" className="border border-divider divide-y divide-divider overflow-hidden">
              {(domains ?? []).length === 0 ? (
                <p className="px-4 py-6 text-center text-neutral-400">{t("domains.section.empty")}</p>
              ) : (
                (domains ?? []).map((d) => (
                  <div key={d.id} data-testid="domain-row" className="flex items-center gap-3 px-4 py-3.5">
                    <div className="grid h-9 w-9 flex-none place-items-center rounded-[10px] bg-neutral-800 text-neutral-300">
                      <GlobeIcon />
                    </div>
                    <div className="flex-1 text-[15px] font-medium text-text">{d.hostname}</div>
                    <div className="text-right text-xs text-neutral-400">
                      <div>{t("domains.section.createdAtLabel")}</div>
                      <div className="mt-0.5 text-[13px] text-text">{formatDateTime(d.created_at, i18n.language)}</div>
                    </div>
                    {canManage ? (
                      <Tooltip label={t("domains.section.deleteTooltip")}>
                        <Button
                          variant="ghost"
                          aria-label={t("domains.section.deleteTooltip")}
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
        title={t("domains.section.deleteDialog.title")}
        description={
          removeTarget
            ? t("domains.section.deleteDialog.description", { hostname: removeTarget.hostname })
            : undefined
        }
        footer={
          <>
            <Button variant="secondary" onClick={() => setRemoveTarget(null)}>
              {t("common.cancel")}
            </Button>
            <Button variant="primary" onClick={confirmRemove} disabled={deleteDomain.isPending}>
              {t("domains.section.deleteDialog.confirmButton")}
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
