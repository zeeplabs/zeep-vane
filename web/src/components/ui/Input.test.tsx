import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { Input } from "./Input";

describe("Input", () => {
  it("renderiza com borda divider e min-height 36px (h-9)", () => {
    render(<Input aria-label="email" />);
    const input = screen.getByRole("textbox", { name: "email" });
    expect(input.className).toContain("border-divider");
    expect(input.className).toContain("min-h-9");
  });

  it("aceita foco e usa borda accent no focus (classe presente)", () => {
    render(<Input aria-label="senha" />);
    const input = screen.getByRole("textbox", { name: "senha" });
    expect(input.className).toContain("focus:border-accent");
  });
});
