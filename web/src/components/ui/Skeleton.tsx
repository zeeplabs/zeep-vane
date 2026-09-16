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
 * pulsing div. */
export function Skeleton({ width, height, radius, className = "" }: SkeletonProps) {
  const style: CSSProperties = {};
  if (width !== undefined) style.width = width;
  if (height !== undefined) style.height = height;
  if (radius !== undefined) style.borderRadius = radius;

  return (
    <div
      data-testid="skeleton"
      style={style}
      className={`bg-neutral-200 dark:bg-neutral-800 animate-pulse motion-reduce:animate-none ${
        radius === undefined ? "rounded-md" : ""
      } ${className}`.trim()}
    />
  );
}
