import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { useSidebarPin } from "./useSidebarPin";

const PIN_KEY = "vane:sidebar-pinned";

beforeEach(() => {
  window.localStorage.clear();
});

afterEach(() => {
  window.localStorage.clear();
  vi.restoreAllMocks();
});

describe("useSidebarPin", () => {
  it("default (no stored value) is pinned: false", () => {
    const { result } = renderHook(() => useSidebarPin());
    expect(result.current.pinned).toBe(false);
  });

  it("togglePinned() flips pinned and persists to localStorage", () => {
    const { result } = renderHook(() => useSidebarPin());

    act(() => {
      result.current.togglePinned();
    });
    expect(result.current.pinned).toBe(true);
    expect(window.localStorage.getItem(PIN_KEY)).toBe("true");

    act(() => {
      result.current.togglePinned();
    });
    expect(result.current.pinned).toBe(false);
    expect(window.localStorage.getItem(PIN_KEY)).toBe("false");
  });

  it("a localStorage failure falls back silently to in-memory state", () => {
    vi.spyOn(window.localStorage.__proto__, "setItem").mockImplementation(() => {
      throw new Error("quota exceeded");
    });
    const { result } = renderHook(() => useSidebarPin());

    expect(() => {
      act(() => {
        result.current.togglePinned();
      });
    }).not.toThrow();
    expect(result.current.pinned).toBe(true);
  });
});
