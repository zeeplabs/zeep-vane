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

  // SPEC_DEVIATION: this assertion previously required OKLCH for the 3
  // semantic tokens (Nocturne). new-layout-migration (spec.md AC4, SHELL-04)
  // replaces those with the handoff's fixed hex values, identical in both
  // themes - OKLCH is no longer the format the spec mandates, so the
  // assertion is updated to match the new spec rather than the old
  // implementation it used to describe.
  it("define os 3 tokens semânticos com os valores hex do handoff, idênticos nos dois temas", () => {
    const css = compiledCss();
    expect(css).toMatch(/--color-success:\s*#1a9e6b/i);
    expect(css).toMatch(/--color-warning:\s*#b45309/i);
    expect(css).toMatch(/--color-critical:\s*#d6395b/i);
  });

  it("define a ramp neutral e accent (100–900) na fonte de tokens.css", () => {
    for (const step of [100, 200, 300, 400, 500, 600, 700, 800, 900]) {
      expect(tokensSource).toContain(`--color-neutral-${step}:`);
      expect(tokensSource).toContain(`--color-accent-${step}:`);
    }
  });

  // SHELL-01/02/03: the spec pins exact literals (handoff-new-layout/
  // README.md's Dark mode + Design tokens tables). jsdom can't resolve
  // @theme or the [data-theme="dark"] cascade, so these assert the authored
  // declarations in the raw source, scoped to the block that owns them -
  // a wrong hex/font typo now fails the suite instead of only being caught
  // by eye (lessons L-037).
  function cssBlock(selector: string): string {
    const start = tokensSource.indexOf(selector);
    if (start === -1) throw new Error(`tokens.css: selector ${selector} not found`);
    const open = tokensSource.indexOf("{", start);
    const close = tokensSource.indexOf("}", open);
    if (open === -1 || close === -1) throw new Error(`tokens.css: unbalanced block for ${selector}`);
    return tokensSource.slice(open + 1, close);
  }

  it("SHELL-01: tokens do shell com os hex exatos do handoff, claros e escuros", () => {
    const light = cssBlock("@theme");
    const dark = cssBlock('[data-theme="dark"]');
    const shellTokens: { token: string; light: RegExp; dark: RegExp }[] = [
      { token: "--color-bg", light: /--color-bg:\s*#ffffff/i, dark: /--color-bg:\s*#15111f/i },
      { token: "--color-text", light: /--color-text:\s*#1c1526/i, dark: /--color-text:\s*#edebf3/i },
      { token: "--color-divider", light: /--color-divider:\s*#ece9f2/i, dark: /--color-divider:\s*#2c2645/i },
      { token: "--color-sidebar-bg", light: /--color-sidebar-bg:\s*#fafafc/i, dark: /--color-sidebar-bg:\s*#1b1730/i },
      { token: "--color-card-header-bg", light: /--color-card-header-bg:\s*#fafafc/i, dark: /--color-card-header-bg:\s*#241e3d/i },
      {
        token: "--color-sidebar-hover-bg",
        light: /--color-sidebar-hover-bg:\s*#f2f0f6/i,
        dark: /--color-sidebar-hover-bg:\s*rgba\(255,\s*255,\s*255,\s*0\.06\)/i,
      },
      { token: "--color-topbar-icon", light: /--color-topbar-icon:\s*#6e6779/i, dark: /--color-topbar-icon:\s*#b4aec2/i },
    ];
    for (const { token, light: lightRe, dark: darkRe } of shellTokens) {
      expect(light, `${token} (light)`).toMatch(lightRe);
      expect(dark, `${token} (dark)`).toMatch(darkRe);
    }
  });

  it("SHELL-02: carrega Manrope 400/500/600/700 e a usa em heading/body", () => {
    expect(tokensSource).toMatch(/Manrope:wght@400;500;600;700/);
    expect(tokensSource).toMatch(/--font-heading:\s*"Manrope"/);
    expect(tokensSource).toMatch(/--font-body:\s*"Manrope"/);
  });

  it("SHELL-03: accent #5a46c7 / hover #4c3aae, sem override no tema escuro", () => {
    const light = cssBlock("@theme");
    const dark = cssBlock('[data-theme="dark"]');
    expect(light).toMatch(/--color-accent:\s*#5a46c7/i);
    expect(light).toMatch(/--color-accent-hover:\s*#4c3aae/i);
    expect(dark).not.toMatch(/--color-accent:/);
    expect(dark).not.toMatch(/--color-accent-hover:/);
  });
});
