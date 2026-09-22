import * as RadixDialog from "@radix-ui/react-dialog";
import { useTranslation } from "react-i18next";
import { MdClose, MdOutlineOpenInNew } from "react-icons/md";
import { buttonVariantClasses, buttonBaseClasses } from "../../components/ui/Button";
import type { StatusPage } from "../../types/api";
import { useDomains } from "../domains/hooks";
import { useServices } from "../services/hooks";

// publicUrl mirrors StatusPagesTable.tsx's own publicUrl()/DSP-17
// null-safety guard, but returns null (not "—") here since the caller uses
// the null-ness to decide whether "View public page" renders at all.
function publicUrl(page: StatusPage, hostname: string | undefined): string | null {
  if (!page.domain_id || !page.subdomain || !hostname) return null;
  return `https://${page.subdomain}.${hostname}`;
}

export interface StatusPageDetailDrawerProps {
  page: StatusPage | null;
  onClose: () => void;
  /** Opens the `EditStatusPageDrawer` for this page - the detail drawer
   * no longer navigates to the separate `/status-pages/{id}` screen, at
   * Julio's explicit request ("didn't look good in a separate screen"). */
  onEdit: (pageId: string) => void;
}

/** Read-only detail drawer for the Status Pages tab (spec.md
 * DSP-14/17): list of attached services, linked domain, "View public
 * page" link (absent with no domain), "Preview public page"
 * (authenticated preview endpoint, works even with no domain attached or
 * before publishing - AD-008; moved here on 2026-09-17, at Julio's
 * request: it previously only existed in the edit drawer) and "Edit page"
 * which opens the `EditStatusPageDrawer` (drawer-based editing, same
 * pattern as creation).
 *
 * Doesn't use the shared <Drawer>, same reason as `DomainDetailDrawer`/
 * `ServiceDetailDrawer`: the mock has no bordered title/footer, it's a
 * continuous panel with a visibility badge+X at the top. */
export function StatusPageDetailDrawer({ page, onClose, onEdit }: StatusPageDetailDrawerProps) {
  const { t } = useTranslation();
  // SPEC_DEVIATION: fixed page 1 for now - only used to resolve
  // domain hostname / service names for this drawer, not a picker; Pager UI
  // is out of scope. Mirrors the same deviation in StatusPagesTable.tsx.
  const { data: domainsPage } = useDomains(1);
  const domain = page ? domainsPage?.items.find((d) => d.id === page.domain_id) : undefined;
  const { data: servicesPage } = useServices(1);
  const services = servicesPage?.items;

  const url = page ? publicUrl(page, domain?.hostname) : null;
  const serviceNames = page ? page.service_ids.map((id) => services?.find((s) => s.id === id)?.name ?? id) : [];

  return (
    <RadixDialog.Root
      open={page !== null}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <RadixDialog.Portal>
        <RadixDialog.Overlay className="fixed inset-0 z-40 bg-black/60 data-[state=closed]:animate-[drawer-overlay-out_180ms_ease-in] data-[state=open]:animate-[drawer-overlay-in_200ms_ease-out]" />
        <RadixDialog.Content className="fixed right-0 top-0 z-50 flex h-full w-full max-w-md flex-col overflow-y-auto bg-surface p-6 shadow-lg outline-none data-[state=closed]:animate-[drawer-panel-out_220ms_ease-in] data-[state=open]:animate-[drawer-panel-in_260ms_cubic-bezier(0.16,1,0.3,1)]">
          {page ? (
            <div className="flex flex-col gap-4">
              <div className="flex items-start justify-between">
                <span
                  className="inline-flex items-center rounded-full px-2.5 py-1 text-xs font-bold"
                  style={{ backgroundColor: "var(--color-accent-100)", color: "var(--color-accent)" }}
                >
                  {t("statusPages.publicTag")}
                </span>
                <RadixDialog.Close asChild>
                  <button type="button" aria-label={t("common.close")} className="cursor-pointer text-text-muted hover:text-text">
                    <MdClose size={18} aria-hidden="true" />
                  </button>
                </RadixDialog.Close>
              </div>

              <div className="-mt-1">
                <RadixDialog.Title asChild>
                  <h2 className="text-[19px] font-bold text-text">{page.name}</h2>
                </RadixDialog.Title>
                {url ? (
                  <a
                    href={url}
                    target="_blank"
                    rel="noreferrer"
                    className="mt-0.5 inline-block font-mono text-[13px] font-semibold text-accent hover:underline"
                  >
                    {domain?.hostname ? `${page.subdomain}.${domain.hostname}` : url}
                  </a>
                ) : null}
              </div>

              <div>
                <div className="mb-2 text-[10.5px] font-bold uppercase tracking-wide text-text-muted">
                  {t("statusPages.detail.servicesLabel", { count: serviceNames.length })}
                </div>
                {serviceNames.length === 0 ? (
                  <div className="text-sm text-text">—</div>
                ) : (
                  <div className="flex flex-col gap-1.5">
                    {serviceNames.map((name, i) => (
                      <div
                        key={`${name}-${i}`}
                        className="flex items-center gap-2.5 rounded-[9px] border border-divider px-3 py-2.5"
                      >
                        <span
                          className="h-1.5 w-1.5 flex-none rounded-full"
                          style={{ backgroundColor: "var(--color-success)" }}
                          aria-hidden="true"
                        />
                        <span className="text-[13px] font-semibold text-text">{name}</span>
                      </div>
                    ))}
                  </div>
                )}
              </div>

              <div>
                <div className="mb-1 text-[10.5px] font-bold uppercase tracking-wide text-text-muted">
                  {t("statusPages.detail.associatedDomainLabel")}
                </div>
                <div className="font-mono text-sm font-semibold text-text">{domain?.hostname ?? "—"}</div>
              </div>

              <a
                href={`/status/${page.id}`}
                target="_blank"
                rel="noreferrer"
                className={`${buttonBaseClasses} ${buttonVariantClasses.secondary} w-fit`}
              >
                <MdOutlineOpenInNew size={14} aria-hidden="true" />
                {t("statusPages.detail.previewButton")}
              </a>

              <div className="flex gap-2.5">
                {url ? (
                  <a
                    href={url}
                    target="_blank"
                    rel="noreferrer"
                    className={`${buttonBaseClasses} ${buttonVariantClasses.secondary} flex-1`}
                  >
                    {t("statusPages.detail.viewPublicButton")}
                  </a>
                ) : null}
                <button
                  type="button"
                  onClick={() => onEdit(page.id)}
                  className={`${buttonBaseClasses} ${buttonVariantClasses.primary} flex-1`}
                >
                  {t("statusPages.detail.editButton")}
                </button>
              </div>
            </div>
          ) : null}
        </RadixDialog.Content>
      </RadixDialog.Portal>
    </RadixDialog.Root>
  );
}
