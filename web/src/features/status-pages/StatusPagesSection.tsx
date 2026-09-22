import { useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { MdOutlineWeb, MdOutlineDeleteOutline, MdOutlineAdd } from "react-icons/md";
import { Card } from "../../components/ui/Card";
import { Dialog } from "../../components/ui/Dialog";
import { Drawer, drawerFooterPrimaryStyle, drawerFooterSecondaryStyle } from "../../components/ui/Drawer";
import { Pager } from "../../components/ui/Pager";
import { Button } from "../../components/ui/Button";
import { Field } from "../../components/ui/Field";
import { Tag } from "../../components/ui/Tag";
import { Tooltip } from "../../components/ui/Tooltip";
import { Skeleton } from "../../components/ui/Skeleton";
import { useAuth } from "../../auth/AuthProvider";
import { ApiError } from "../../lib/apiClient";
import type { StatusPage } from "../../types/api";
import { useDomains } from "../domains/hooks";
import { useServices } from "../services/hooks";
import { useCreateStatusPage, useDeleteStatusPage, useStatusPages } from "./hooks";

function LayoutIcon() {
  return <MdOutlineWeb size={17} aria-hidden="true" />;
}

function TrashIcon() {
  return <MdOutlineDeleteOutline size={15} aria-hidden="true" />;
}

// publicUrl only composes a URL once both domain_id/subdomain are set
// (SPD-01) - a domain-less page is always in "draft" state (attaching a
// domain is the only way to leave that state), so this null-safe guard
// is defensive: it never renders a broken "https://null.undefined".
function publicUrl(page: StatusPage, hostname: string | undefined): string | null {
  if (!page.domain_id || !page.subdomain) return null;
  return `https://${page.subdomain}.${hostname ?? "?"}`;
}

/** Status pages table + dialog. Shared between `DomainsStatusPagesPage` (handoff shows both sections on the same screen) and `StatusPagesPage` (its own route, same pattern as `ServicesSection`). */
export function StatusPagesSection() {
  const { t } = useTranslation();
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
      else setRemoveError(t("statusPages.section.deleteError"));
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
      else setError(t("statusPages.section.createError"));
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
          <Tag variant="success">{t("statusPages.section.publishedTag")}</Tag>
        </div>
      );
    }
    if (p.state === "tls_failed") {
      return (
        <div className="flex flex-col items-end gap-0.5">
          <Tag variant="critical">{t("statusPages.section.failedTag")}</Tag>
          <span className="text-xs text-neutral-400">{p.tls_last_error}</span>
        </div>
      );
    }
    // SPD-12: no domain attached yet - distinct from the "waiting for
    // DNS/certificate" state below, with an action to leave this state.
    // Same logic as StatusPageDetail.tsx, applied to the list.
    if (p.domain_id === null) {
      return (
        <div className="flex flex-col items-end gap-1">
          <Tag variant="accent-outline" className="w-fit">
            {t("statusPages.section.noDomainTag")}
          </Tag>
          <Link to={`/status-pages/${p.id}`} className="text-xs text-accent hover:underline">
            {t("statusPages.section.attachDomainLink")}
          </Link>
        </div>
      );
    }
    // SPD-13: domain attached, but the certificate hasn't been issued
    // yet - replaces the old ambiguous "Issuing certificate" text, which
    // didn't distinguish this case from "no domain" above.
    return (
      <Tag variant="accent" data-testid="pulsing-tag" className="animate-pulse">
        {t("statusPages.section.pendingTag")}
      </Tag>
    );
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <h4 className="text-text">{t("statusPages.section.title")}</h4>
        {canManage ? (
          <Button
            variant="primary"
            onClick={() => {
              resetForm();
              setDialogOpen(true);
            }}
          >
            <MdOutlineAdd size={14} aria-hidden="true" />
            {t("statusPages.section.createButton")}
          </Button>
        ) : null}
      </div>

      <div>
        {isLoading ? (
          <div aria-busy="true" className="rounded-md border border-divider divide-y divide-divider overflow-hidden">
            <span className="sr-only">{t("statusPages.loading")}</span>
            {Array.from({ length: 3 }).map((_, i) => (
              <div key={i} className="flex items-center gap-3 px-4 py-3.5">
                <Skeleton width={36} height={36} radius={10} />
                <div className="flex-1">
                  <Skeleton width={140} height={14} />
                  <Skeleton width={90} height={12} className="mt-1.5" />
                </div>
                <Skeleton width={80} height={20} radius={999} />
              </div>
            ))}
          </div>
        ) : (
          <>
            <Card elevation="none" className="border border-divider divide-y divide-divider overflow-hidden">
              {(pages ?? []).length === 0 ? (
                <p className="px-4 py-6 text-center text-neutral-400">{t("statusPages.section.empty")}</p>
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
                      <Tooltip label={t("statusPages.section.deleteTooltip")}>
                        <Button
                          variant="ghost"
                          aria-label={t("statusPages.section.deleteTooltip")}
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
        title={t("statusPages.section.deleteDialog.title")}
        description={
          removeTarget
            ? t("statusPages.section.deleteDialog.description", { name: removeTarget.name })
            : undefined
        }
        footer={
          <>
            <Button variant="secondary" onClick={() => setRemoveTarget(null)}>
              {t("common.cancel")}
            </Button>
            <Button variant="primary" onClick={confirmRemove} disabled={deleteStatusPage.isPending}>
              {t("statusPages.section.deleteDialog.confirmButton")}
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
        title={t("statusPages.createTitle")}
        description={t("statusPages.section.createDescription")}
        closeLabel={t("common.close")}
        footer={
          <>
            <Button
              type="button"
              variant="secondary"
              style={drawerFooterSecondaryStyle}
              onClick={() => setDialogOpen(false)}
            >
              {t("common.cancel")}
            </Button>
            <Button
              type="submit"
              form="create-status-page-form"
              variant="solid"
              style={drawerFooterPrimaryStyle}
              disabled={createStatusPage.isPending}
            >
              {t("statusPages.section.submitButton")}
            </Button>
          </>
        }
      >
        <form id="create-status-page-form" onSubmit={handleSubmit} className="flex flex-col gap-3">
          <Field label={t("statusPages.section.nameLabel")} value={name} onChange={(e) => setName(e.target.value)} required />
          <div className="flex flex-col gap-1">
            <span className="text-sm font-medium text-text">{t("statusPages.section.servicesLabel")}</span>
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
