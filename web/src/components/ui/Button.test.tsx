import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Button } from "./Button";

describe("Button", () => {
  it("renderiza variante primary sem preenchimento sólido (outline)", () => {
    render(<Button>Entrar</Button>);
    const btn = screen.getByRole("button", { name: "Entrar" });
    expect(btn).toHaveAttribute("data-variant", "primary");
    expect(btn.className).toContain("border-accent");
    expect(btn.className).not.toContain("bg-accent ");
  });

  it("renderiza variante secondary com borda divider", () => {
    render(<Button variant="secondary">Cancelar</Button>);
    expect(screen.getByRole("button", { name: "Cancelar" })).toHaveAttribute(
      "data-variant",
      "secondary"
    );
  });

  it("renderiza variante ghost sem borda", () => {
    render(<Button variant="ghost">Sair</Button>);
    const btn = screen.getByRole("button", { name: "Sair" });
    expect(btn.className).toContain("border-0");
  });

  it("renderiza variante icon sem label, 36x36", () => {
    render(<Button variant="icon" aria-label="fechar" />);
    const btn = screen.getByRole("button", { name: "fechar" });
    expect(btn.className).toContain("w-9");
    expect(btn.className).toContain("h-9");
  });

  it("respeita disabled e não dispara onClick", async () => {
    const onClick = vi.fn();
    render(
      <Button disabled onClick={onClick}>
        Desabilitado
      </Button>
    );
    const btn = screen.getByRole("button", { name: "Desabilitado" });
    expect(btn).toBeDisabled();
    await userEvent.click(btn);
    expect(onClick).not.toHaveBeenCalled();
  });
});
