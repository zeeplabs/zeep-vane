import { useCallback, useState } from "react";

// vane:sidebar-pinned - persisted "Fixar menu" preference (new-layout-
// migration, SHELL-14). Sibling of useThemeToggle's localStorage pattern -
// two tiny hooks, no shared abstraction (design.md's explicit choice).
const SIDEBAR_PIN_STORAGE_KEY = "vane:sidebar-pinned";

function readStoredPinned(): boolean {
  try {
    return window.localStorage.getItem(SIDEBAR_PIN_STORAGE_KEY) === "true";
  } catch {
    // localStorage unavailable (private mode, quota) - degrade to
    // not-pinned without throwing (spec.md Edge Case).
    return false;
  }
}

/**
 * "Fixar menu" (sidebar pin) state + toggle, persisted in
 * `localStorage["vane:sidebar-pinned"]` (new-layout-migration, SHELL-14).
 */
export function useSidebarPin(): { pinned: boolean; togglePinned: () => void } {
  const [pinned, setPinned] = useState<boolean>(() => readStoredPinned());

  const togglePinned = useCallback(() => {
    setPinned((current) => {
      const next = !current;
      try {
        window.localStorage.setItem(SIDEBAR_PIN_STORAGE_KEY, String(next));
      } catch {
        // localStorage unavailable - pin state still updates in memory
        // (spec.md Edge Case: no visible error to the user).
      }
      return next;
    });
  }, []);

  return { pinned, togglePinned };
}
