import * as RadixDialog from "@radix-ui/react-dialog";
import { MdClose, MdOutlineOpenInNew } from "react-icons/md";
import { buttonVariantClasses, buttonBaseClasses } from "../../components/ui/Button";
import type { StatusPage } from "../../types/api";
import { useDomains } from "../domains/hooks";
import { useServices } from "../services/hooks";

// publicUrl mirrors StatusPagesTable.tsx's own publicUrl()/DSP-17
// null-safety guard, but returns null (not "—") here since the caller uses
// the null-ness to decide whether "Ver página pública" renders at all.
function publicUrl(page: StatusPage, hostname: string | undefined): string | null {
  if (!page.domain_id || !page.subdomain || !hostname) return null;
  return `https://${page.subdomain}.${hostname}`;
}

export interface StatusPageDetailDrawerProps {
  page: StatusPage | null;
  onClose: () => void;
  /** Abre o `EditStatusPageDrawer` para esta página - o drawer de detalhe
   * não navega mais para a tela separada `/status-pages/{id}`, a pedido
   * explícito do Julio ("em tela separada nao ficou legal"). */
  onEdit: (pageId: string) => void;
}

/** Drawer de detalhe (somente leitura) da aba Status Pages (spec.md
 * DSP-14/17): lista de serviços anexados, domínio vinculado, link "Ver
 * página pública" (ausente sem domínio), "Pré-visualizar página pública"
 * (endpoint de preview autenticado, funciona mesmo sem domínio anexado ou
 * antes de publicar - AD-008; movido pra cá em 2026-09-17, a pedido do
 * Julio: antes só existia no drawer de edição) e "Editar página" que abre
 * o `EditStatusPageDrawer` (edição em drawer, mesmo modelo do de criação).
 *
 * Não usa o <Drawer> compartilhado, mesmo motivo de `DomainDetailDrawer`/
 * `ServiceDetailDrawer`: o mock não tem título/rodapé com borda, é um
 * painel contínuo com badge de visibilidade+X no topo. */
export function StatusPageDetailDrawer({ page, onClose, onEdit }: StatusPageDetailDrawerProps) {
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
                  Público
                </span>
                <RadixDialog.Close asChild>
                  <button type="button" aria-label="Fechar" className="cursor-pointer text-text-muted hover:text-text">
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
                  Serviços exibidos ({serviceNames.length})
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
                  Domínio associado
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
                Pré-visualizar página pública
              </a>

              <div className="flex gap-2.5">
                {url ? (
                  <a
                    href={url}
                    target="_blank"
                    rel="noreferrer"
                    className={`${buttonBaseClasses} ${buttonVariantClasses.secondary} flex-1`}
                  >
                    Ver página pública
                  </a>
                ) : null}
                <button
                  type="button"
                  onClick={() => onEdit(page.id)}
                  className={`${buttonBaseClasses} ${buttonVariantClasses.primary} flex-1`}
                >
                  Editar página
                </button>
              </div>
            </div>
          ) : null}
        </RadixDialog.Content>
      </RadixDialog.Portal>
    </RadixDialog.Root>
  );
}
