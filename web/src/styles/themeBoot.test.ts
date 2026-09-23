import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { applyBootTheme } from "./themeBoot";

// SHELL-07: theme-boot runs as a regular module imported at the top of
// main.tsx (not an inline <script> in index.html, which the app's own CSP
// - default-src 'self', internal/api/security_headers.go - silently blocks
// in production; see themeBoot.ts's own header comment for the incident
// this regression test guards against).
describe("applyBootTheme (SHELL-07)", () => {
  beforeEach(() => {
    delete document.documentElement.dataset.theme;
  });

  afterEach(() => {
    delete document.documentElement.dataset.theme;
    vi.restoreAllMocks();
  });

  it("applies data-theme=dark when the persisted theme is dark", () => {
    vi.spyOn(window.localStorage.__proto__, "getItem").mockReturnValue("dark");

    applyBootTheme();

    expect(document.documentElement.dataset.theme).toBe("dark");
  });

  it("does not set data-theme when nothing is persisted (light default)", () => {
    vi.spyOn(window.localStorage.__proto__, "getItem").mockReturnValue(null);

    applyBootTheme();

    expect(document.documentElement.dataset.theme).toBeUndefined();
  });

  it("does not set data-theme when the persisted value is light", () => {
    vi.spyOn(window.localStorage.__proto__, "getItem").mockReturnValue("light");

    applyBootTheme();

    expect(document.documentElement.dataset.theme).toBeUndefined();
  });

  it("does not throw and keeps the light default if localStorage fails", () => {
    vi.spyOn(window.localStorage.__proto__, "getItem").mockImplementation(() => {
      throw new Error("localStorage unavailable");
    });

    expect(() => applyBootTheme()).not.toThrow();
    expect(document.documentElement.dataset.theme).toBeUndefined();
  });
});
