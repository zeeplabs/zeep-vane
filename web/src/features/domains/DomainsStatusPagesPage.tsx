import { DomainsSection } from "./DomainsSection";
import { StatusPagesSection } from "../status-pages/StatusPagesSection";

export function DomainsStatusPagesPage() {
  return (
    <div className="mx-auto flex w-full max-w-[1280px] flex-col gap-8">
      <div>
        <h2 className="text-text">Domínios & Status Pages</h2>
        <p className="m-0 text-[13.5px] text-neutral-400">
          Cadastre domínios raiz e publique status pages em subdomínios com TLS automático.
        </p>
      </div>

      <DomainsSection />
      <StatusPagesSection />
    </div>
  );
}
