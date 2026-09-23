import { useTranslation } from "react-i18next";
import { Drawer } from "../../components/ui/Drawer";
import { useStatusPage } from "./hooks";
import { StatusPageEditorContent } from "./StatusPageEditorContent";

export interface EditStatusPageDrawerProps {
  pageId: string | null;
  onClose: () => void;
}

/** Drawer de edição de status page do novo layout - substitui a navegação
 * pra tela separada (`/status-pages/{id}`, `StatusPageDetail.tsx`), a
 * pedido explícito do Julio: "quero manter a edicao de status page em
 * drawer, igual é feito hoje na criacao, pois em tela separada nao ficou
 * legal". Reusa o mesmo corpo de edição (`StatusPageEditorContent`) que a
 * tela legada usa, só troca o wrapper - todas as ações reais (anexar
 * domínio, verificar DNS/certificado, salvar serviços) continuam
 * idênticas, não é um formulário único com um botão de submit.
 *
 * `showPreviewLink={false}`: o link "Pré-visualizar página pública" saiu
 * daqui (2026-09-17, pedido do Julio) - ficava só no drawer de edição, e
 * o lugar certo é o `StatusPageDetailDrawer` ("visualizar detalhes"). A
 * tela legada `/status-pages/{id}` continua mostrando (default `true`),
 * já que ela não tem um drawer de detalhe separado. */
export function EditStatusPageDrawer({ pageId, onClose }: EditStatusPageDrawerProps) {
  const { t } = useTranslation();
  const { data: page } = useStatusPage(pageId ?? "");

  return (
    <Drawer
      open={pageId !== null}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
      title={page?.name ?? t("statusPages.edit.defaultTitle")}
      description={t("statusPages.edit.description")}
      closeLabel={t("common.close")}
    >
      {page ? <StatusPageEditorContent page={page} showPreviewLink={false} /> : null}
    </Drawer>
  );
}
