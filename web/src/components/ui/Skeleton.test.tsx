import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { Skeleton } from "./Skeleton";

describe("Skeleton", () => {
  it("renders a sized div with data-testid=skeleton (SKEL-01/03)", () => {
    render(<Skeleton width={120} height={16} />);
    const el = screen.getByTestId("skeleton");
    expect(el.style.width).toBe("120px");
    expect(el.style.height).toBe("16px");
  });

  it("applies animate-pulse and motion-reduce:animate-none (SKEL-01/02)", () => {
    render(<Skeleton width={120} height={16} />);
    const el = screen.getByTestId("skeleton");
    expect(el.className).toContain("animate-pulse");
    expect(el.className).toContain("motion-reduce:animate-none");
  });

  it("uses the bg-skeleton token, theme-aware via [data-theme] instead of Tailwind's dark: (SKEL-01, theme)", () => {
    render(<Skeleton width={40} height={40} />);
    const el = screen.getByTestId("skeleton");
    expect(el.className).toContain("bg-skeleton");
    expect(el.className).not.toContain("dark:");
  });

  it("uses rounded-md by default when radius is not provided", () => {
    render(<Skeleton width={40} height={40} />);
    const el = screen.getByTestId("skeleton");
    expect(el.className).toContain("rounded-md");
    expect(el.style.borderRadius).toBe("");
  });

  it("applies custom radius via inline style when provided", () => {
    render(<Skeleton width={40} height={40} radius={999} />);
    const el = screen.getByTestId("skeleton");
    expect(el.style.borderRadius).toBe("999px");
    expect(el.className).not.toContain("rounded-md");
  });

  it("accepts a string for width/height/radius", () => {
    render(<Skeleton width="100%" height="1rem" radius="0.5rem" />);
    const el = screen.getByTestId("skeleton");
    expect(el.style.width).toBe("100%");
    expect(el.style.height).toBe("1rem");
    expect(el.style.borderRadius).toBe("0.5rem");
  });

  it("merges additional className without replacing the default classes", () => {
    render(<Skeleton width={40} height={40} className="mb-2" />);
    const el = screen.getByTestId("skeleton");
    expect(el.className).toContain("mb-2");
    expect(el.className).toContain("animate-pulse");
  });
});
