import { useCallback, useState } from "react";

// vane:theme - persisted theme preference (new-layout-migration, SHELL-06).
// Read synchronously (before React mounts) by the theme-boot script in
// web/index.html; read/written here at runtime by the toggle.
const THEME_STORAGE_KEY = "vane:theme";

export type Theme = "light" | "dark";

function readStoredTheme(): Theme {
  try {
    return window.localStorage.getItem(THEME_STORAGE_KEY) === "dark" ? "dark" : "light";
  } catch {
    // localStorage unavailable (private mode, quota) - degrade to the light
    // default without throwing (spec.md Edge Case).
    return "light";
  }
}

function applyTheme(theme: Theme): void {
  if (theme === "dark") {
    document.documentElement.dataset.theme = "dark";
  } else {
    delete document.documentElement.dataset.theme;
  }
}

/**
 * Current theme + toggle, synchronized with `document.documentElement`'s
 * `data-theme` attribute and `localStorage["vane:theme"]`
 * (new-layout-migration, SHELL-06).
 */
export function useThemeToggle(): { theme: Theme; toggleTheme: () => void } {
  const [theme, setTheme] = useState<Theme>(() => readStoredTheme());

  const toggleTheme = useCallback(() => {
    setTheme((current) => {
      const next: Theme = current === "dark" ? "light" : "dark";
      applyTheme(next);
      try {
        window.localStorage.setItem(THEME_STORAGE_KEY, next);
      } catch {
        // localStorage unavailable - theme still updates in memory/DOM
        // (spec.md Edge Case: no visible error to the user).
      }
      return next;
    });
  }, []);

  return { theme, toggleTheme };
}
