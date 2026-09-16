import { useCallback, useState } from "react";

// vane:publicStatusTheme - deliberately separate from the app's own
// `vane:theme` (useThemeToggle) and never touches document.documentElement
// (public-status-redesign, PUBSTATUS-07..10). This toggle is scoped to a
// single public status page: an anonymous visitor's choice here must never
// leak into (or be affected by) the admin panel's theme, and vice versa -
// the value is applied via `data-theme` on this page's own root element,
// which tokens.css's `[data-theme="dark"]` selector already cascades into
// regardless of which element carries the attribute.
const STORAGE_KEY = "vane:publicStatusTheme";

export type PublicStatusTheme = "light" | "dark";

function readStoredTheme(): PublicStatusTheme {
  try {
    return window.localStorage.getItem(STORAGE_KEY) === "dark" ? "dark" : "light";
  } catch {
    // localStorage unavailable (private mode, quota) - degrade to the light
    // default without throwing (spec.md Edge Case, mirrors useThemeToggle).
    return "light";
  }
}

/**
 * Theme state local to the public status page (PUBSTATUS-07..10) - defaults
 * to light on first visit (never reads `prefers-color-scheme` or the app's
 * `vane:theme`), persists per-visitor in its own localStorage key.
 */
export function usePublicStatusTheme(): { theme: PublicStatusTheme; toggleTheme: () => void } {
  const [theme, setTheme] = useState<PublicStatusTheme>(() => readStoredTheme());

  const toggleTheme = useCallback(() => {
    setTheme((current) => {
      const next: PublicStatusTheme = current === "dark" ? "light" : "dark";
      try {
        window.localStorage.setItem(STORAGE_KEY, next);
      } catch {
        // localStorage unavailable - theme still updates in memory (Edge Case).
      }
      return next;
    });
  }, []);

  return { theme, toggleTheme };
}
