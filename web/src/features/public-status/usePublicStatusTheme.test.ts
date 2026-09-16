import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { usePublicStatusTheme } from "./usePublicStatusTheme";

const STORAGE_KEY = "vane:publicStatusTheme";

beforeEach(() => {
  window.localStorage.clear();
});

afterEach(() => {
  window.localStorage.clear();
  vi.restoreAllMocks();
});

describe("usePublicStatusTheme", () => {
  it("default (no stored value) is light", () => {
    const { result } = renderHook(() => usePublicStatusTheme());
    expect(result.current.theme).toBe("light");
  });

  it("a localStorage read failure degrades to light without throwing", () => {
    vi.spyOn(window.localStorage.__proto__, "getItem").mockImplementation(() => {
      throw new Error("quota exceeded");
    });

    let result: ReturnType<typeof usePublicStatusTheme> | undefined;
    expect(() => {
      result = renderHook(() => usePublicStatusTheme()).result.current;
    }).not.toThrow();
    expect(result?.theme).toBe("light");
  });

  it("a localStorage write failure does not throw out of toggleTheme() and still updates in-memory state", () => {
    vi.spyOn(window.localStorage.__proto__, "setItem").mockImplementation(() => {
      throw new Error("quota exceeded");
    });
    const { result } = renderHook(() => usePublicStatusTheme());

    expect(() => {
      act(() => {
        result.current.toggleTheme();
      });
    }).not.toThrow();
    expect(result.current.theme).toBe("dark");
  });

  it("toggleTheme() persists under its own storage key", () => {
    const { result } = renderHook(() => usePublicStatusTheme());

    act(() => {
      result.current.toggleTheme();
    });

    expect(window.localStorage.getItem(STORAGE_KEY)).toBe("dark");
  });
});
