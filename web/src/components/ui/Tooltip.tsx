import type { ReactNode } from "react";

export interface TooltipProps {
  label: string;
  children: ReactNode;
}

/** Tooltip CSS-only (sem estado/JS): mostra `label` no hover/focus do filho. Usar em botões, principalmente os só-ícone, onde o nome acessível (aria-label) não aparece visualmente. */
export function Tooltip({ label, children }: TooltipProps) {
  return (
    <span className="group/tooltip relative inline-flex">
      {children}
      <span
        role="tooltip"
        className="pointer-events-none absolute left-1/2 top-full z-50 mt-1.5 -translate-x-1/2 whitespace-nowrap rounded-md border border-divider bg-surface px-2 py-1 text-xs text-text opacity-0 shadow-md transition-opacity delay-150 group-hover/tooltip:opacity-100 group-focus-within/tooltip:opacity-100"
      >
        {label}
      </span>
    </span>
  );
}
