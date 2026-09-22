import { describe, it, expect } from "vitest";
import { formatDateTime, formatDateTimeShort } from "./formatDate";

const ISO = "2026-03-05T14:30:00.000Z";

describe("formatDateTime", () => {
  it("formats using the given locale, not a fixed one", () => {
    const ptBR = formatDateTime(ISO, "pt-BR");
    const enUS = formatDateTime(ISO, "en-US");
    expect(ptBR).not.toBe(enUS);
  });
});

describe("formatDateTimeShort", () => {
  it("formats using the given locale, not a fixed one", () => {
    const ptBR = formatDateTimeShort(ISO, "pt-BR");
    const enUS = formatDateTimeShort(ISO, "en-US");
    expect(ptBR).not.toBe(enUS);
  });

  it("includes day/month/hour/minute, not seconds or year", () => {
    const formatted = formatDateTimeShort(ISO, "en-US");
    expect(formatted).not.toMatch(/2026/);
  });
});
