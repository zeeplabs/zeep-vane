import { Link } from "react-router-dom";
import { Drawer, drawerFooterPrimaryStyle, drawerFooterSecondaryStyle } from "../../components/ui/Drawer";
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
}

/** Drawer de detalhe (somente leitura) da aba Status Pages (spec.md
 * DSP-14/17): lista de serviços anexados, domínio vinculado, link "Ver
 * página pública" (ausente sem domínio) e "Editar página" apontando para a
 * tela de edição existente (`/status-pages/{id}`). */
export function StatusPageDetailDrawer({ page, onClose }: StatusPageDetailDrawerProps) {
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
    <Drawer
      open={page !== null}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
      title={page?.name ?? ""}
      closeLabel="Fechar"
      footer={
        page ? (
          <>
            {url ? (
              <a
                href={url}
                target="_blank"
                rel="noreferrer"
                style={drawerFooterSecondaryStyle}
                className={`${buttonBaseClasses} ${buttonVariantClasses.secondary}`}
              >
                Ver página pública
              </a>
            ) : null}
            <Link
              to={`/status-pages/${page.id}`}
              style={drawerFooterPrimaryStyle}
              className={`${buttonBaseClasses} ${buttonVariantClasses.solid}`}
            >
              Editar página
            </Link>
          </>
        ) : null
      }
    >
      {page ? (
        <div className="flex flex-col gap-4">
          <div>
            <div className="text-xs text-text-muted">Serviços</div>
            {serviceNames.length === 0 ? (
              <div className="text-sm text-text">—</div>
            ) : (
              <ul className="mt-1 flex flex-col gap-1">
                {serviceNames.map((name, i) => (
                  <li key={`${name}-${i}`} className="text-sm text-text">
                    {name}
                  </li>
                ))}
              </ul>
            )}
          </div>
          <div>
            <div className="text-xs text-text-muted">Domínio</div>
            <div className="text-sm text-text">{domain?.hostname ?? "—"}</div>
          </div>
        </div>
      ) : null}
    </Drawer>
  );
}
