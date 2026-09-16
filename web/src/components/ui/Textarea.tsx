import { forwardRef, type TextareaHTMLAttributes } from "react";
import type { InputVariant } from "./Input";

export interface TextareaProps extends TextareaHTMLAttributes<HTMLTextAreaElement> {
  variant?: InputVariant;
}

// Same variant classes as Input - "default" (bordered white box) or
// "filled" (borderless tinted bg, handoff-new-layout's textarea style,
// e.g. Incidentes' "Descreva o progresso..."). Kept in sync with Input.tsx
// intentionally rather than importing its private class map, since the
// two share the visual language but not a DOM element.
const variantClasses: Record<InputVariant, string> = {
  default: "border-divider bg-surface focus:border-accent",
  filled: "border-transparent bg-card-header-bg focus:border-accent focus:bg-surface",
};

export const Textarea = forwardRef<HTMLTextAreaElement, TextareaProps>(function Textarea(
  { variant = "default", className = "", ...props },
  ref
) {
  return (
    <textarea
      ref={ref}
      data-variant={variant}
      className={
        "w-full resize-y rounded-md border text-text px-3 py-2.5 text-sm " +
        "outline-none transition-colors " +
        variantClasses[variant] +
        " " +
        className
      }
      {...props}
    />
  );
});
