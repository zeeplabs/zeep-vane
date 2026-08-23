import { useState } from "react";
import { useParams } from "react-router-dom";
import { Tag } from "../../components/ui/Tag";
import { Button } from "../../components/ui/Button";
import { useDomains } from "../domains/hooks";
import { useStatusPage } from "./hooks";
import { AttachDomainDrawer } from "./AttachDomainDrawer";

// publicUrl only composes a URL once both domain_id/subdomain are set
// (SPD-01) - null-safe, same guard as StatusPagesSection.tsx's publicUrl.
function publicUrl(domainId: string | null, subdomain: string | null, hostname: string | undefined): string | null {
  if (!domainId || !subdomain) return null;
  return `https://${subdomain}.${hostname ?? "?"}`;
}

export function StatusPageDetail() {
  const { id = "" } = useParams();
  const { data: page, isLoading } = useStatusPage(id);
  const { data: domains } = useDomains();
  const [attachOpen, setAttachOpen] = useState(false);

  if (isLoading) return <p className="text-neutral-400">Carregando…</p>;
  if (!page) return <p className="text-neutral-400">Status page não encontrada.</p>;

  const hostname = domains?.find((d) => d.id === page.domain_id)?.hostname;
  const url = publicUrl(page.domain_id, page.subdomain, hostname);

  return (
    <div className="mx-auto flex w-full max-w-[1280px] flex-col gap-4">
      <h3 className="text-text">{page.name}</h3>

      {/* SPD-12: sem domínio nenhum anexado ainda - distinto do "aguardando
          DNS/certificado" abaixo, com uma ação pra sair desse estado. */}
      {page.domain_id === null ? (
        <div className="flex flex-col gap-2">
          <Tag variant="accent-outline" className="w-fit">
            Sem domínio configurado
          </Tag>
          <Button variant="secondary" className="w-fit" onClick={() => setAttachOpen(true)}>
            Anexar domínio
          </Button>
        </div>
      ) : null}

      {/* SPD-13: domínio anexado, mas o certificado ainda não foi emitido -
          substitui o antigo texto ambíguo "Emitindo certificado", que não
          distinguia esse caso do de "sem domínio" acima. */}
      {page.domain_id !== null && page.state === "draft" ? (
        <Tag variant="accent" className="w-fit animate-pulse">
          Aguardando validação de DNS/certificado
        </Tag>
      ) : null}

      {page.state === "published" ? (
        <div className="flex flex-col gap-1">
          <Tag variant="success" className="w-fit">
            Publicada
          </Tag>
          {url ? (
            <a href={url} target="_blank" rel="noreferrer" className="text-sm text-accent hover:underline">
              {url}
            </a>
          ) : null}
        </div>
      ) : null}

      {page.state === "tls_failed" ? (
        <div className="flex flex-col gap-1">
          <Tag variant="critical" className="w-fit">
            Falha
          </Tag>
          <p className="text-sm text-neutral-400">{page.tls_last_error}</p>
        </div>
      ) : null}

      {/* SPD-01/SPD-14: sempre visível, independente de state/domain - o
          preview (`public-preview`) já compõe pra qualquer estado (AD-008),
          então não há motivo pra escondê-lo aqui. */}
      <a
        href={`/status/${page.id}`}
        target="_blank"
        rel="noreferrer"
        className="w-fit text-sm text-accent hover:underline"
      >
        Pré-visualizar página pública
      </a>

      <AttachDomainDrawer statusPageId={page.id} open={attachOpen} onOpenChange={setAttachOpen} />
    </div>
  );
}
