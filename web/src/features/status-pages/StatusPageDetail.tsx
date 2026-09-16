import { useParams } from "react-router-dom";
import { useStatusPage } from "./hooks";
import { StatusPageEditorContent } from "./StatusPageEditorContent";

/** Tela legada `/status-pages/{id}` - ainda usada pelo fluxo pré-redesign
 * (`StatusPagesPage`/`StatusPagesSection`). O fluxo do novo layout
 * (`DomainsStatusPagesPage`) usa `EditStatusPageDrawer` em vez de navegar
 * pra cá - "em tela separada nao ficou legal" (feedback do Julio). O corpo
 * real de edição vive em `StatusPageEditorContent`, compartilhado pelos
 * dois. */
export function StatusPageDetail() {
  const { id = "" } = useParams();
  const { data: page, isLoading } = useStatusPage(id);

  if (isLoading) return <p className="text-neutral-400">Carregando…</p>;
  if (!page) return <p className="text-neutral-400">Status page não encontrada.</p>;

  return (
    <div className="mx-auto flex w-full max-w-[1280px] flex-col gap-4">
      <StatusPageEditorContent page={page} />
    </div>
  );
}
