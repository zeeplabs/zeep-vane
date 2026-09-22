import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { renderHook, act } from "@testing-library/react";
import i18n from "./i18n";
import { useLanguage } from "./useLanguage";

const LANGUAGE_KEY = "vane:language";

beforeEach(() => {
  window.localStorage.clear();
});

afterEach(async () => {
  window.localStorage.clear();
  await i18n.changeLanguage("pt-BR");
  vi.restoreAllMocks();
});

describe("useLanguage", () => {
  it("default (no stored value) is pt-BR", () => {
    const { result } = renderHook(() => useLanguage());
    expect(result.current.language).toBe("pt-BR");
  });

  it("reads a stored en-US preference as the initial language", () => {
    window.localStorage.setItem(LANGUAGE_KEY, "en-US");
    const { result } = renderHook(() => useLanguage());
    expect(result.current.language).toBe("en-US");
  });

  it("setLanguage() updates state, changes i18next's language, and persists to localStorage", async () => {
    const { result } = renderHook(() => useLanguage());

    await act(async () => {
      result.current.setLanguage("en-US");
    });

    expect(result.current.language).toBe("en-US");
    expect(i18n.language).toBe("en");
    expect(window.localStorage.getItem(LANGUAGE_KEY)).toBe("en-US");

    await act(async () => {
      result.current.setLanguage("pt-BR");
    });

    expect(result.current.language).toBe("pt-BR");
    expect(i18n.language).toBe("pt-BR");
    expect(window.localStorage.getItem(LANGUAGE_KEY)).toBe("pt-BR");
  });

  it("a localStorage write failure does not throw out of setLanguage()", async () => {
    vi.spyOn(window.localStorage.__proto__, "setItem").mockImplementation(() => {
      throw new Error("quota exceeded");
    });
    const { result } = renderHook(() => useLanguage());

    await expect(
      act(async () => {
        result.current.setLanguage("en-US");
      })
    ).resolves.not.toThrow();
    // Language still updates in memory/i18next even though persistence failed.
    expect(result.current.language).toBe("en-US");
    expect(i18n.language).toBe("en");
  });
});
