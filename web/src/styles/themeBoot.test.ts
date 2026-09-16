// @vitest-environment node
import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

// SHELL-07: index.html's inline theme-boot script runs before React mounts,
// so a jsdom render (which mounts React) can never observe its timing. This
// evaluates the script's own logic in isolation against fake window/document
// objects - the closest thing to a unit test for pre-mount behavior
// (lessons L-039). A regression in the script (wrong storage key, applying
// the theme to the wrong element, or not swallowing a localStorage throw)
// now fails the suite instead of only being caught by eye.
function bootScript(): string {
  const html = readFileSync(fileURLToPath(new URL("../../index.html", import.meta.url)), "utf8");
  const match = html.match(/<script>([\s\S]*?)<\/script>/);
  if (!match) throw new Error("index.html: inline theme-boot script not found");
  return match[1];
}

function runBoot(getItem: () => string | null): Record<string, string> {
  const document = { documentElement: { dataset: {} as Record<string, string> } };
  const window = { localStorage: { getItem } };
  new Function("window", "document", bootScript())(window, document);
  return document.documentElement.dataset;
}

describe("index.html theme-boot (SHELL-07)", () => {
  it("aplica data-theme=dark quando o tema persistido é dark", () => {
    expect(runBoot(() => "dark").theme).toBe("dark");
  });

  it("não define data-theme quando nada está persistido (padrão claro)", () => {
    expect(runBoot(() => null).theme).toBeUndefined();
  });

  it("não define data-theme quando o persistido é light", () => {
    expect(runBoot(() => "light").theme).toBeUndefined();
  });

  it("não lança e mantém o padrão claro se localStorage falhar", () => {
    const dataset = runBoot(() => {
      throw new Error("localStorage unavailable");
    });
    expect(dataset.theme).toBeUndefined();
  });
});
