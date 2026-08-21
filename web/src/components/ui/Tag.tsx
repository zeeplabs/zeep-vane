import type { HTMLAttributes } from "react";

export type TagVariant =
  | "accent"
  | "accent-outline"
  | "neutral"
  | "neutral-outline"
  | "success"
  | "warning"
  | "critical";

export interface TagProps extends HTMLAttributes<HTMLSpanElement> {
  variant?: TagVariant;
}

const base =
  "inline-flex items-center rounded-sm px-2 py-0.5 text-xs font-medium leading-5 whitespace-nowrap";

const variantClasses: Record<TagVariant, string> = {
  accent: "bg-accent-900 text-accent-200 border border-transparent",
  "accent-outline": "bg-transparent text-accent border border-accent",
  neutral: "bg-neutral-800 text-neutral-200 border border-transparent",
  "neutral-outline": "bg-transparent text-neutral-300 border border-neutral-600",
  success: "border border-transparent",
  warning: "border border-transparent",
  critical: "border border-transparent",
};

const semanticStyle: Partial<Record<TagVariant, React.CSSProperties>> = {
  success: {
    backgroundColor: "color-mix(in oklch, var(--color-success) 18%, transparent)",
    color: "var(--color-success)",
  },
  warning: {
    backgroundColor: "color-mix(in oklch, var(--color-warning) 18%, transparent)",
    color: "var(--color-warning)",
  },
  critical: {
    backgroundColor: "color-mix(in oklch, var(--color-critical) 18%, transparent)",
    color: "var(--color-critical)",
  },
};

export function Tag({ variant = "neutral", className = "", style, ...props }: TagProps) {
  return (
    <span
      data-variant={variant}
      className={`${base} ${variantClasses[variant]} ${className}`.trim()}
      style={{ ...semanticStyle[variant], ...style }}
      {...props}
    />
  );
}
