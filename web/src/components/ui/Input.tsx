import { forwardRef, type InputHTMLAttributes } from "react";

export type InputVariant = "default" | "filled";

export interface InputProps extends InputHTMLAttributes<HTMLInputElement> {
  variant?: InputVariant;
}

const variantClasses: Record<InputVariant, string> = {
  default: "border-divider bg-surface focus:border-accent",
  // Borderless tinted-bg field (handoff-new-layout's search-box style,
  // e.g. "Buscar serviço") - distinct from "default"'s visibly bordered
  // white box used everywhere pre-redesign. Reused across every
  // new-layout-migration screen instead of a raw <input> per page.
  filled: "border-transparent bg-card-header-bg focus:border-accent focus:bg-surface",
};

export const Input = forwardRef<HTMLInputElement, InputProps>(function Input(
  { variant = "default", className = "", ...props },
  ref
) {
  return (
    <input
      ref={ref}
      data-variant={variant}
      className={
        "w-full min-h-9 rounded-md border text-text px-3 text-sm " +
        "outline-none transition-colors " +
        variantClasses[variant] +
        " " +
        className
      }
      {...props}
    />
  );
});
