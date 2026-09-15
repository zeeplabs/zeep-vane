import type { CSSProperties, ReactNode } from "react";
import * as RadixDialog from "@radix-ui/react-dialog";
import { MdClose } from "react-icons/md";

// Shared inline styles for the two-button Cancelar/CTA footer pattern every
// standardized drawer uses (handoff-new-layout's add-service drawer mock:
// flex:1 equal-width buttons, 9px radius, 13.5px text - all values this
// app's shared Button variant classes don't already produce, so overridden
// via style, which reliably wins over Button's own classes regardless of
// Tailwind's class-emission order). Exported so every drawer's footer reads
// from the same values instead of re-typing them per screen.
export const drawerFooterSecondaryStyle: CSSProperties = {
  flex: 1,
  height: "auto",
  padding: "11px",
  borderRadius: 9,
  fontSize: 13.5,
  fontWeight: 600,
};
export const drawerFooterPrimaryStyle: CSSProperties = {
  ...drawerFooterSecondaryStyle,
  fontWeight: 700,
};

export interface DrawerProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description?: string;
  children?: ReactNode;
  footer?: ReactNode;
  /** Accessible name for the header's close icon button - the caller
   * supplies its own translated string (Drawer itself is feature-agnostic,
   * no i18n namespace of its own). */
  closeLabel: string;
}

/** Painel lateral (slide da direita), mesma base Radix do `Dialog`. Usar no
 * lugar do `Dialog` quando o conteúdo tem lista rolável de tamanho variável
 * (ex.: seleção de serviços) que não cabe confortavelmente num modal centralizado.
 *
 * Chrome fixo (headerless-border, X de fechar, padding 24px, footer com
 * border-top) - o mesmo modelo em todo o sistema, por decisão explícita do
 * usuário: nenhum drawer pode ter um chrome diferente dos outros
 * (`handoff-new-layout/Servicos Monitorados.dc.html`'s add-service drawer é
 * a referência canônica). Não reintroduzir props de opt-out por tela. */
export function Drawer({ open, onOpenChange, title, description, children, footer, closeLabel }: DrawerProps) {
  return (
    <RadixDialog.Root open={open} onOpenChange={onOpenChange}>
      <RadixDialog.Portal>
        <RadixDialog.Overlay className="fixed inset-0 z-40 bg-black/60" />
        <RadixDialog.Content className="fixed right-0 top-0 z-50 flex h-full w-full max-w-md flex-col bg-surface shadow-lg outline-none">
          <div className="p-[24px] pb-[6px]">
            <div className="flex items-start justify-between gap-4">
              <div>
                <RadixDialog.Title className="text-lg font-bold text-text">{title}</RadixDialog.Title>
                {description ? (
                  <RadixDialog.Description className="mt-[6px] text-sm text-neutral-400">
                    {description}
                  </RadixDialog.Description>
                ) : null}
              </div>
              <RadixDialog.Close asChild>
                <button
                  type="button"
                  aria-label={closeLabel}
                  className="mt-0.5 flex-none cursor-pointer text-neutral-400 hover:text-text"
                >
                  <MdClose size={18} aria-hidden="true" />
                </button>
              </RadixDialog.Close>
            </div>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto p-[24px] pt-[18px]">{children}</div>
          {footer ? (
            <div className="flex gap-[10px] border-t border-divider px-[24px] py-[16px]">{footer}</div>
          ) : null}
        </RadixDialog.Content>
      </RadixDialog.Portal>
    </RadixDialog.Root>
  );
}
