import type { CSSProperties } from "react";

export interface SkeletonProps {
  /** Inline width. Number is treated as pixels, string is used as-is. */
  width?: number | string;
  /** Inline height. Number is treated as pixels, string is used as-is. */
  height?: number | string;
  /** Corner radius. Defaults to the app's `rounded-md` card radius token when omitted. */
  radius?: number | string;
  className?: string;
}

/** Shared pulsing placeholder block used by every in-scope screen's loading
 * state (loading-skeletons spec, SKEL-01..03). Screens compose their own
 * loaded-layout shape from one or more of these instead of reimplementing a
 * pulsing div. Uses the theme-aware `--color-skeleton` token ([data-theme]
 * driven) instead of Tailwind's `dark:` variant, which keys off OS
 * prefers-color-scheme and would ignore the app's own theme toggle. */
export function Skeleton({ width, height, radius, className = "" }: SkeletonProps) {
  const style: CSSProperties = {};
  if (width !== undefined) style.width = width;
  if (height !== undefined) style.height = height;
  if (radius !== undefined) style.borderRadius = radius;

  return (
    <div
      data-testid="skeleton"
      style={style}
      className={`bg-skeleton animate-pulse motion-reduce:animate-none ${
        radius === undefined ? "rounded-md" : ""
      } ${className}`.trim()}
    />
  );
}
