// Theme-boot: applies the persisted theme to <html> before React mounts, so
// the first paint never flashes the opposite theme (new-layout-migration,
// SHELL-05/SHELL-07). Reads the same "vane:theme" localStorage key
// useThemeToggle reads/writes at runtime. Wrapped in try/catch - a throw
// (private-mode restriction, quota) is swallowed and the app boots with the
// light default, matching the Edge Case in spec.md.
//
// Lives as a regular module imported by main.tsx (not an inline <script> in
// index.html) because the app's own Content-Security-Policy
// (internal/api/security_headers.go, `default-src 'self'`) blocks inline
// script execution in production - an inline boot script never ran there,
// so the DOM stayed on the light default while useThemeToggle's initial
// React state still read "dark" from localStorage, desyncing the topbar
// icon from the actual rendered theme until two toggles resynced them.
export function applyBootTheme(): void {
  try {
    if (window.localStorage.getItem("vane:theme") === "dark") {
      document.documentElement.dataset.theme = "dark";
    }
  } catch {
    // localStorage unavailable - fall back to the light default.
  }
}
