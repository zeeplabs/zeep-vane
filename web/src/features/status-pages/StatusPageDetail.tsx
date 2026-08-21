import { useParams } from "react-router-dom";
import { Tag } from "../../components/ui/Tag";
import { useDomains } from "../domains/hooks";
import { useStatusPage } from "./hooks";

export function StatusPageDetail() {
  const { id = "" } = useParams();
  const { data: page, isLoading } = useStatusPage(id);
  const { data: domains } = useDomains();

  if (isLoading) return <p className="text-neutral-400">Carregando…</p>;
  if (!page) return <p className="text-neutral-400">Status page não encontrada.</p>;

  const hostname = domains?.find((d) => d.id === page.domain_id)?.hostname;
  const url = `https://${page.subdomain}.${hostname ?? "?"}`;

  return (
    <div className="mx-auto flex w-full max-w-[1280px] flex-col gap-4">
      <h3 className="text-text">{page.name}</h3>

      {page.state === "draft" ? (
        <Tag variant="accent" className="animate-pulse w-fit">
          Emitindo certificado
        </Tag>
      ) : null}

      {page.state === "published" ? (
        <div className="flex flex-col gap-1">
          <Tag variant="success" className="w-fit">
            Publicada
          </Tag>
          <a href={url} target="_blank" rel="noreferrer" className="text-sm text-accent hover:underline">
            {url}
          </a>
          <a href={`/status/${page.id}`} target="_blank" rel="noreferrer" className="text-sm text-accent hover:underline">
            Pré-visualizar página pública (mock)
          </a>
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
    </div>
  );
}
