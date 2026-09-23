import { describe, it, expect } from "vitest";
import { formatRelativeTime, formatDateTime, formatDuration } from "./format";

// Minimal fake translator - just enough to prove formatRelativeTime picks
// the right key/count and composes what the (fake) translation returns,
// without depending on the real locale JSON files or i18next.
function fakeT(key: string, opts?: Record<string, unknown>): string {
  const count = opts?.count;
  return count === undefined ? key : `${key}:${count}`;
}

describe("formatRelativeTime", () => {
  it("returns publicStatus.now for under a minute", () => {
    const now = Date.parse("2026-03-05T14:30:00.000Z");
    expect(formatRelativeTime("2026-03-05T14:29:45.000Z", fakeT, now)).toBe("publicStatus.now");
  });

  it("uses the minute key with count for under an hour", () => {
    const now = Date.parse("2026-03-05T14:30:00.000Z");
    expect(formatRelativeTime("2026-03-05T14:15:00.000Z", fakeT, now)).toBe(
      "publicStatus.relativeTime.minute:15",
    );
  });

  it("uses the hour key with count for under a day", () => {
    const now = Date.parse("2026-03-05T14:30:00.000Z");
    expect(formatRelativeTime("2026-03-05T11:30:00.000Z", fakeT, now)).toBe(
      "publicStatus.relativeTime.hour:3",
    );
  });

  it("uses the day key with count at 24h or more", () => {
    const now = Date.parse("2026-03-05T14:30:00.000Z");
    expect(formatRelativeTime("2026-03-02T14:30:00.000Z", fakeT, now)).toBe(
      "publicStatus.relativeTime.day:3",
    );
  });
});

describe("formatDateTime", () => {
  it("formats using the given locale, not a fixed one", () => {
    const iso = "2026-03-05T14:30:00.000Z";
    expect(formatDateTime(iso, "pt-BR")).not.toBe(formatDateTime(iso, "en-US"));
  });
});

describe("formatDuration", () => {
  it("returns bare minutes under an hour", () => {
    expect(formatDuration("2026-03-05T14:00:00.000Z", "2026-03-05T14:05:00.000Z")).toBe("5min");
  });

  it("returns bare hours on an exact-hour boundary", () => {
    expect(formatDuration("2026-03-05T14:00:00.000Z", "2026-03-05T16:00:00.000Z")).toBe("2h");
  });

  it("returns hours and minutes combined", () => {
    expect(formatDuration("2026-03-05T14:00:00.000Z", "2026-03-05T16:05:00.000Z")).toBe("2h5min");
  });
});
