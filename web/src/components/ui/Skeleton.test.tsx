import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { Skeleton } from "./Skeleton";

describe("Skeleton", () => {
  it("renderiza um div dimensionado com data-testid=skeleton (SKEL-01/03)", () => {
    render(<Skeleton width={120} height={16} />);
    const el = screen.getByTestId("skeleton");
    expect(el.style.width).toBe("120px");
    expect(el.style.height).toBe("16px");
  });

  it("aplica animate-pulse e motion-reduce:animate-none (SKEL-01/02)", () => {
    render(<Skeleton width={120} height={16} />);
    const el = screen.getByTestId("skeleton");
    expect(el.className).toContain("animate-pulse");
    expect(el.className).toContain("motion-reduce:animate-none");
  });

  it("usa o token bg-skeleton, theme-aware via [data-theme] em vez do dark: do Tailwind (SKEL-01, tema)", () => {
    render(<Skeleton width={40} height={40} />);
    const el = screen.getByTestId("skeleton");
    expect(el.className).toContain("bg-skeleton");
    expect(el.className).not.toContain("dark:");
  });

  it("usa rounded-md por padrão quando radius não é informado", () => {
    render(<Skeleton width={40} height={40} />);
    const el = screen.getByTestId("skeleton");
    expect(el.className).toContain("rounded-md");
    expect(el.style.borderRadius).toBe("");
  });

  it("aplica radius customizado via style inline quando informado", () => {
    render(<Skeleton width={40} height={40} radius={999} />);
    const el = screen.getByTestId("skeleton");
    expect(el.style.borderRadius).toBe("999px");
    expect(el.className).not.toContain("rounded-md");
  });

  it("aceita string em width/height/radius", () => {
    render(<Skeleton width="100%" height="1rem" radius="0.5rem" />);
    const el = screen.getByTestId("skeleton");
    expect(el.style.width).toBe("100%");
    expect(el.style.height).toBe("1rem");
    expect(el.style.borderRadius).toBe("0.5rem");
  });

  it("mescla className adicional sem substituir as classes padrão", () => {
    render(<Skeleton width={40} height={40} className="mb-2" />);
    const el = screen.getByTestId("skeleton");
    expect(el.className).toContain("mb-2");
    expect(el.className).toContain("animate-pulse");
  });
});
