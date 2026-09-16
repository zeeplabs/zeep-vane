import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { useThemeToggle } from "./useThemeToggle";

const THEME_KEY = "vane:theme";

beforeEach(() => {
  window.localStorage.clear();
  delete document.documentElement.dataset.theme;
});

afterEach(() => {
  window.localStorage.clear();
  delete document.documentElement.dataset.theme;
  vi.restoreAllMocks();
});

describe("useThemeToggle", () => {
  it("default (no stored value) is light", () => {
    const { result } = renderHook(() => useThemeToggle());
    expect(result.current.theme).toBe("light");
  });

  it("reads a stored dark preference as the initial theme", () => {
    window.localStorage.setItem(THEME_KEY, "dark");
    const { result } = renderHook(() => useThemeToggle());
    expect(result.current.theme).toBe("dark");
  });

  it("toggleTheme() flips theme, updates document.documentElement.dataset.theme, and persists to localStorage", () => {
    const { result } = renderHook(() => useThemeToggle());

    act(() => {
      result.current.toggleTheme();
    });

    expect(result.current.theme).toBe("dark");
    expect(document.documentElement.dataset.theme).toBe("dark");
    expect(window.localStorage.getItem(THEME_KEY)).toBe("dark");

    act(() => {
      result.current.toggleTheme();
    });

    expect(result.current.theme).toBe("light");
    expect(document.documentElement.dataset.theme).toBeUndefined();
    expect(window.localStorage.getItem(THEME_KEY)).toBe("light");
  });

  it("a localStorage write failure does not throw out of toggleTheme()", () => {
    vi.spyOn(window.localStorage.__proto__, "setItem").mockImplementation(() => {
      throw new Error("quota exceeded");
    });
    const { result } = renderHook(() => useThemeToggle());

    expect(() => {
      act(() => {
        result.current.toggleTheme();
      });
    }).not.toThrow();
    // Theme still updates in memory/DOM even though persistence failed.
    expect(result.current.theme).toBe("dark");
    expect(document.documentElement.dataset.theme).toBe("dark");
  });
});
