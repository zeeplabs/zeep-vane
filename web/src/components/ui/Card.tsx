import type { HTMLAttributes } from "react";

export type CardElevation = "elev-sm" | "elev-md" | "elev-lg";

export interface CardProps extends HTMLAttributes<HTMLDivElement> {
  elevation?: CardElevation;
}

const elevationClasses: Record<CardElevation, string> = {
  "elev-sm": "shadow-sm",
  "elev-md": "shadow-md",
  "elev-lg": "shadow-lg",
};

export function Card({ elevation = "elev-sm", className = "", ...props }: CardProps) {
  return (
    <div
      data-elevation={elevation}
      className={`rounded-md bg-surface ${elevationClasses[elevation]} ${className}`.trim()}
      {...props}
    />
  );
}
