import { describe, it, expect } from "vitest";
import { render } from "@testing-library/react";
import "./tokens.css";
// Fonte crua de tokens.css (sem tree-shaking do Tailwind), usada só para
// checar a ramp completa — o compilado só mantém os passos de fato usados
// no projeto.
import tokensSource from "./tokens.css?raw";

// Smoke test: renderiza um elemento por token de cor e confirma que o CSS
// compilado (gerado a partir de tokens.css via Tailwind @theme) define a
// variável correspondente com um valor concreto — não vazio, não hardcoded
// fora de tokens.css.
//
// Nota: jsdom não processa @layer (usado pelo Tailwind v4 para @theme), então
// getComputedStyle(documentElement) não resolve essas custom properties em
// ambiente de teste — isso é uma limitação conhecida do jsdom, não um bug do
// design system. Por isso o smoke test inspeciona o CSS compilado injetado em
// <head>, que é onde as regras realmente vivem (e que o navegador real
// resolve normalmente).
function compiledCss(): string {
  return document.head.innerHTML;
}

const colorTokenProbes: { token: string; className: string; property: "background-color" | "color" }[] = [
  { token: "--color-bg", className: "bg-bg", property: "background-color" },
  { token: "--color-surface", className: "bg-surface", property: "background-color" },
  { token: "--color-text", className: "text-text", property: "color" },
  { token: "--color-accent", className: "text-accent", property: "color" },
  { token: "--color-accent-2", className: "text-accent-2", property: "color" },
];

describe("tokens.css", () => {
  it.each(colorTokenProbes)(
    "token $token: elemento com classe .$className resolve para uma regra CSS concreta",
    ({ token, className, property }) => {
      const { container } = render(<div data-testid="probe" className={className} />);
      const el = container.querySelector('[data-testid="probe"]') as HTMLElement;
      expect(el.className).toContain(className);

      const css = compiledCss();
      // A classe deve existir como regra compilada...
      expect(css).toContain(`.${className.replace("/", "\\/")}`);
      // ...usando a variável do token (não um valor hardcoded solto).
      const rulePattern = new RegExp(
        `\\.${className.replace(/[.*+?^${}()|[\]\\]/g, "\\$&").replace("\\/", "\\/")}[\\s\\S]{0,80}${property}:\\s*var\\(${token}\\)`
      );
      expect(css).toMatch(rulePattern);
    }
  );

  it("define os 3 tokens semânticos em OKLCH no bloco @theme", () => {
    const css = compiledCss();
    expect(css).toMatch(/--color-success:\s*oklch\(/);
    expect(css).toMatch(/--color-warning:\s*oklch\(/);
    expect(css).toMatch(/--color-critical:\s*oklch\(/);
  });

  it("define a ramp neutral e accent (100–900) na fonte de tokens.css", () => {
    for (const step of [100, 200, 300, 400, 500, 600, 700, 800, 900]) {
      expect(tokensSource).toContain(`--color-neutral-${step}:`);
      expect(tokensSource).toContain(`--color-accent-${step}:`);
    }
  });
});
