import { forwardRef, type ButtonHTMLAttributes } from "react";

export type ButtonVariant = "primary" | "secondary" | "ghost" | "icon";

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant;
}

// Exported alongside buttonVariantClasses (see below) for the same reason.
export const buttonBaseClasses =
  "inline-flex items-center justify-center gap-2 rounded-md text-sm font-medium transition-colors cursor-pointer " +
  "disabled:opacity-45 disabled:pointer-events-none focus-visible:outline-none";

// Exported so non-<button> elements that must look like a Button (e.g. an
// <a> that opens an external link and needs real link semantics/role) can
// reuse the same classes instead of hand-duplicating them out of sync.
export const buttonVariantClasses: Record<ButtonVariant, string> = {
  primary:
    "border border-accent text-accent bg-transparent px-4 h-9 " +
    "hover:bg-accent-900 active:bg-accent-800",
  secondary:
    "border border-divider text-text bg-transparent px-4 h-9 " +
    "hover:bg-neutral-900 active:bg-neutral-800",
  ghost: "border-0 text-accent bg-transparent px-2 h-9 hover:text-accent-2 active:text-accent-2",
  icon: "border border-divider text-text bg-transparent w-9 h-9 p-0 hover:bg-neutral-900 active:bg-neutral-800",
};

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  { variant = "primary", className = "", ...props },
  ref
) {
  return (
    <button
      ref={ref}
      data-variant={variant}
      className={`${buttonBaseClasses} ${buttonVariantClasses[variant]} ${className}`.trim()}
      {...props}
    />
  );
});
