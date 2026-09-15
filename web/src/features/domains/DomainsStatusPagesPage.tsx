import { useState } from "react";
import { MdOutlineAdd } from "react-icons/md";
import { useAuth } from "../../auth/AuthProvider";
import { Button } from "../../components/ui/Button";
import type { Domain, StatusPage } from "../../types/api";
import { AddDomainDrawer } from "./AddDomainDrawer";
import { DomainDetailDrawer } from "./DomainDetailDrawer";
import { DomainsTable } from "./DomainsTable";
import { AddStatusPageDrawer } from "../status-pages/AddStatusPageDrawer";
import { StatusPageDetailDrawer } from "../status-pages/StatusPageDetailDrawer";
import { StatusPagesTable } from "../status-pages/StatusPagesTable";

type Tab = "domains" | "status-pages";

/** Tela `/domains` redesenhada (spec.md DSP-18/19): switcher de duas abas
 * (Domínios/Status Pages), cada uma com sua própria tabela + drawer de
 * detalhe + drawer de adição (T6-T11). `DomainsSection`/`StatusPagesSection`
 * seguem intocados - continuam em uso por `IntegrationsPage` (design.md's
 * "genuinely different layout" precedent). */
export function DomainsStatusPagesPage() {
  const { hasRole } = useAuth();
  const canManage = hasRole(["owner", "operator"]);

  const [tab, setTab] = useState<Tab>("domains");
  const [selectedDomain, setSelectedDomain] = useState<Domain | null>(null);
  const [selectedPage, setSelectedPage] = useState<StatusPage | null>(null);
  const [addOpen, setAddOpen] = useState(false);

  // DSP-19: switching tabs must not leak drawer state from the previous
  // tab - both tables' detail/add drawers reset here, not just on unmount,
  // since `addOpen` is shared by both tabs' add-drawer.
  function switchTab(next: Tab) {
    setTab(next);
    setSelectedDomain(null);
    setSelectedPage(null);
    setAddOpen(false);
  }

  return (
    <div className="mx-auto flex w-full max-w-[1280px] flex-col gap-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h2 className="text-text">Domínios & Status Pages</h2>
          <p className="m-0 text-[13.5px] text-neutral-400">
            Gerencie os domínios verificados e as páginas públicas de status do Vane.
          </p>
        </div>
        {canManage ? (
          <Button variant="solid" onClick={() => setAddOpen(true)}>
            <MdOutlineAdd size={15} aria-hidden="true" />
            {tab === "domains" ? "Adicionar domínio" : "Criar status page"}
          </Button>
        ) : null}
      </div>

      <div role="tablist" className="flex gap-6 border-b border-divider">
        <button
          type="button"
          role="tab"
          aria-selected={tab === "domains"}
          onClick={() => switchTab("domains")}
          className={
            "border-b-2 px-0.5 py-2.5 text-[13.5px] " +
            (tab === "domains" ? "border-accent font-bold text-text" : "border-transparent font-semibold text-neutral-400")
          }
        >
          Domínios
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={tab === "status-pages"}
          onClick={() => switchTab("status-pages")}
          className={
            "border-b-2 px-0.5 py-2.5 text-[13.5px] " +
            (tab === "status-pages"
              ? "border-accent font-bold text-text"
              : "border-transparent font-semibold text-neutral-400")
          }
        >
          Status Pages
        </button>
      </div>

      {tab === "domains" ? (
        <>
          <DomainsTable onSelect={setSelectedDomain} />
          <DomainDetailDrawer domain={selectedDomain} onClose={() => setSelectedDomain(null)} />
          <AddDomainDrawer open={addOpen} onOpenChange={setAddOpen} />
        </>
      ) : (
        <>
          <StatusPagesTable onSelect={setSelectedPage} />
          <StatusPageDetailDrawer page={selectedPage} onClose={() => setSelectedPage(null)} />
          <AddStatusPageDrawer open={addOpen} onOpenChange={setAddOpen} />
        </>
      )}
    </div>
  );
}
