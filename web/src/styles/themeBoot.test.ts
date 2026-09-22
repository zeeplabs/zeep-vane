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

  it("aplica data-theme=dark quando o tema persistido é dark", () => {
    vi.spyOn(window.localStorage.__proto__, "getItem").mockReturnValue("dark");

    applyBootTheme();

    expect(document.documentElement.dataset.theme).toBe("dark");
  });

  it("não define data-theme quando nada está persistido (padrão claro)", () => {
    vi.spyOn(window.localStorage.__proto__, "getItem").mockReturnValue(null);

    applyBootTheme();

    expect(document.documentElement.dataset.theme).toBeUndefined();
  });

  it("não define data-theme quando o persistido é light", () => {
    vi.spyOn(window.localStorage.__proto__, "getItem").mockReturnValue("light");

    applyBootTheme();

    expect(document.documentElement.dataset.theme).toBeUndefined();
  });

  it("não lança e mantém o padrão claro se localStorage falhar", () => {
    vi.spyOn(window.localStorage.__proto__, "getItem").mockImplementation(() => {
      throw new Error("localStorage unavailable");
    });

    expect(() => applyBootTheme()).not.toThrow();
    expect(document.documentElement.dataset.theme).toBeUndefined();
  });
});
