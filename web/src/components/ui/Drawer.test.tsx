import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Drawer } from "./Drawer";

describe("Drawer", () => {
  it("renderiza título, descrição, conteúdo e footer quando aberto", () => {
    render(
      <Drawer
        open
        onOpenChange={() => {}}
        title="Criar status page"
        description="Selecione os serviços"
        footer={<button>Criar</button>}
      >
        <p>corpo</p>
      </Drawer>
    );
    expect(screen.getByText("Criar status page")).toBeInTheDocument();
    expect(screen.getByText("Selecione os serviços")).toBeInTheDocument();
    expect(screen.getByText("corpo")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Criar" })).toBeInTheDocument();
  });

  it("Escape chama onOpenChange(false)", async () => {
    const onOpenChange = vi.fn();
    render(
      <Drawer open onOpenChange={onOpenChange} title="Criar incidente">
        <p>conteúdo</p>
      </Drawer>
    );
    await userEvent.keyboard("{Escape}");
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("fechado não renderiza conteúdo", () => {
    render(
      <Drawer open={false} onOpenChange={() => {}} title="Criar status page">
        <p>corpo</p>
      </Drawer>
    );
    expect(screen.queryByText("corpo")).not.toBeInTheDocument();
  });
});
