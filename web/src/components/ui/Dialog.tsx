import type { ReactNode } from "react";
import * as RadixDialog from "@radix-ui/react-dialog";

export interface DialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description?: string;
  children?: ReactNode;
  /** Impede fechar clicando fora (backdrop) ou apertando Esc. Necessário
   * pro modal de sessão expirada, que é bloqueante. */
  disableBackdropDismiss?: boolean;
}

export function Dialog({
  open,
  onOpenChange,
  title,
  description,
  children,
  disableBackdropDismiss = false,
}: DialogProps) {
  return (
    <RadixDialog.Root open={open} onOpenChange={onOpenChange}>
      <RadixDialog.Portal>
        <RadixDialog.Overlay className="fixed inset-0 bg-black/60 z-40" />
        <RadixDialog.Content
          className="fixed left-1/2 top-1/2 z-50 w-full max-w-md -translate-x-1/2 -translate-y-1/2 rounded-lg bg-surface p-7 shadow-lg"
          onPointerDownOutside={(e) => {
            if (disableBackdropDismiss) e.preventDefault();
          }}
          onInteractOutside={(e) => {
            if (disableBackdropDismiss) e.preventDefault();
          }}
          onEscapeKeyDown={(e) => {
            if (disableBackdropDismiss) e.preventDefault();
          }}
        >
          <RadixDialog.Title className="text-lg font-medium text-text">{title}</RadixDialog.Title>
          {description ? (
            <RadixDialog.Description className="mt-1 text-sm text-neutral-400">
              {description}
            </RadixDialog.Description>
          ) : null}
          <div className="mt-4">{children}</div>
        </RadixDialog.Content>
      </RadixDialog.Portal>
    </RadixDialog.Root>
  );
}
