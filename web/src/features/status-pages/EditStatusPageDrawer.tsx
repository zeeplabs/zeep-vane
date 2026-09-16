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
 * idênticas, não é um formulário único com um botão de submit. */
export function EditStatusPageDrawer({ pageId, onClose }: EditStatusPageDrawerProps) {
  const { data: page } = useStatusPage(pageId ?? "");

  return (
    <Drawer
      open={pageId !== null}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
      title={page?.name ?? "Editar status page"}
      description="Gerencie domínio, verificação e serviços exibidos nesta página."
      closeLabel="Fechar"
    >
      {page ? <StatusPageEditorContent page={page} /> : null}
    </Drawer>
  );
}
