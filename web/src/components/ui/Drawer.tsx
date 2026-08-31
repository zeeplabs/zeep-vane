import type { ReactNode } from "react";
import * as RadixDialog from "@radix-ui/react-dialog";

export interface DrawerProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description?: string;
  children?: ReactNode;
  footer?: ReactNode;
}

/** Painel lateral (slide da direita), mesma base Radix do `Dialog`. Usar no
 * lugar do `Dialog` quando o conteúdo tem lista rolável de tamanho variável
 * (ex.: seleção de serviços) que não cabe confortavelmente num modal centralizado. */
export function Drawer({ open, onOpenChange, title, description, children, footer }: DrawerProps) {
  return (
    <RadixDialog.Root open={open} onOpenChange={onOpenChange}>
      <RadixDialog.Portal>
        <RadixDialog.Overlay className="fixed inset-0 z-40 bg-black/60" />
        <RadixDialog.Content className="fixed right-0 top-0 z-50 flex h-full w-full max-w-md flex-col bg-surface shadow-lg">
          <div className="border-b border-divider p-6">
            <RadixDialog.Title className="text-lg font-medium text-text">{title}</RadixDialog.Title>
            {description ? (
              <RadixDialog.Description className="mt-1 text-sm text-neutral-400">
                {description}
              </RadixDialog.Description>
            ) : null}
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
