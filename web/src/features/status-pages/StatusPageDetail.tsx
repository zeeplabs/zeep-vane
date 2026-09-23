import { useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Skeleton } from "../../components/ui/Skeleton";
import { useStatusPage } from "./hooks";
import { StatusPageEditorContent } from "./StatusPageEditorContent";

/** Tela legada `/status-pages/{id}` - ainda usada pelo fluxo pré-redesign
 * (`StatusPagesPage`/`StatusPagesSection`). O fluxo do novo layout
 * (`DomainsStatusPagesPage`) usa `EditStatusPageDrawer` em vez de navegar
 * pra cá - "em tela separada nao ficou legal" (feedback do Julio). O corpo
 * real de edição vive em `StatusPageEditorContent`, compartilhado pelos
 * dois. */
export function StatusPageDetail() {
  const { t } = useTranslation();
  const { id = "" } = useParams();
  const { data: page, isLoading } = useStatusPage(id);

  if (isLoading) {
    return (
      <div aria-busy="true" className="mx-auto flex w-full max-w-[1280px] flex-col gap-4">
        <span className="sr-only">{t("statusPages.loading")}</span>
        <div className="flex items-center justify-between">
          <Skeleton width={220} height={20} />
          <Skeleton width={100} height={24} radius={999} />
        </div>
        <Skeleton height={80} />
        <Skeleton height={120} />
        <Skeleton height={120} />
      </div>
    );
  }
  if (!page) return <p className="text-neutral-400">{t("statusPages.detail.notFound")}</p>;

  return (
    <div className="mx-auto flex w-full max-w-[1280px] flex-col gap-4">
      <StatusPageEditorContent page={page} />
    </div>
  );
}
