import type { ReactNode } from "react";
import * as RadixDialog from "@radix-ui/react-dialog";
import { MdClose } from "react-icons/md";

export interface DrawerProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description?: string;
  children?: ReactNode;
  footer?: ReactNode;
  /** Renders an X icon at the header's top-right that closes the drawer
   * (handoff-new-layout's drawer pattern) - omitted by default so
   * pre-redesign screens keep their current header exactly as-is.
   * Requires `closeLabel` for its accessible name. */
  showCloseButton?: boolean;
  /** Accessible name for the close icon button - required when
   * `showCloseButton` is true; the caller supplies its own translated
   * string (Drawer itself is feature-agnostic, no i18n namespace of its own). */
  closeLabel?: string;
  /** Omits the border-bottom under the title/description (handoff-new-
   * layout's drawers have no separator there) - default true (existing
   * behavior) so pre-redesign screens are unaffected. */
  headerBorder?: boolean;
}

/** Painel lateral (slide da direita), mesma base Radix do `Dialog`. Usar no
 * lugar do `Dialog` quando o conteúdo tem lista rolável de tamanho variável
 * (ex.: seleção de serviços) que não cabe confortavelmente num modal centralizado. */
export function Drawer({
  open,
  onOpenChange,
  title,
  description,
  children,
  footer,
  showCloseButton = false,
  closeLabel,
  headerBorder = true,
}: DrawerProps) {
  return (
    <RadixDialog.Root open={open} onOpenChange={onOpenChange}>
      <RadixDialog.Portal>
        <RadixDialog.Overlay className="fixed inset-0 z-40 bg-black/60" />
        <RadixDialog.Content className="fixed right-0 top-0 z-50 flex h-full w-full max-w-md flex-col bg-surface shadow-lg outline-none">
          <div className={`p-6 ${headerBorder ? "border-b border-divider" : ""}`}>
            <div className="flex items-start justify-between gap-4">
              <div>
                <RadixDialog.Title className="text-lg font-medium text-text">{title}</RadixDialog.Title>
                {description ? (
                  <RadixDialog.Description className="mt-1 text-sm text-neutral-400">
                    {description}
                  </RadixDialog.Description>
                ) : null}
              </div>
              {showCloseButton ? (
                <RadixDialog.Close asChild>
                  <button
                    type="button"
                    aria-label={closeLabel}
                    className="mt-0.5 flex-none cursor-pointer text-neutral-400 hover:text-text"
                  >
                    <MdClose size={18} aria-hidden="true" />
                  </button>
                </RadixDialog.Close>
              ) : null}
            </div>
          </div>
          <div className="flex-1 overflow-y-auto p-6">{children}</div>
          {footer ? (
            <div className="flex justify-end gap-2 border-t border-divider p-4">{footer}</div>
          ) : null}
        </RadixDialog.Content>
      </RadixDialog.Portal>
    </RadixDialog.Root>
  );
}
