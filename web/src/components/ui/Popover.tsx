import type { ReactNode } from "react";
import * as RadixPopover from "@radix-ui/react-popover";

export interface PopoverProps {
  /** Elemento que abre o popover ao ser clicado (ou ativado via teclado). */
  trigger: ReactNode;
  children: ReactNode;
}

/**
 * Popover clicável e posicionado (não hover, não tela cheia): abre no clique
 * do `trigger`, fecha no clique fora, Esc, ou clicando o trigger de novo -
 * comportamento padrão do Radix Popover, acessível por teclado de graça
 * (Tab foca o trigger, Enter/Espaço abre, Esc fecha). Construído só para o
 * caso de uso das barras horárias degradadas da status page pública
 * (degraded-interval-analysis) - não é um componente genérico de propósito
 * geral como `Dialog`/`Drawer`.
 */
export function Popover({ trigger, children }: PopoverProps) {
  return (
    <RadixPopover.Root>
      <RadixPopover.Trigger asChild>{trigger}</RadixPopover.Trigger>
      <RadixPopover.Portal>
        <RadixPopover.Content
          className="z-50 w-72 rounded-md border border-divider bg-surface p-3 text-sm text-text shadow-md"
          sideOffset={6}
          collisionPadding={8}
        >
          {children}
          <RadixPopover.Arrow className="fill-surface" />
        </RadixPopover.Content>
      </RadixPopover.Portal>
    </RadixPopover.Root>
  );
}
