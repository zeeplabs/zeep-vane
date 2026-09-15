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
        closeLabel="Fechar"
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
      <Drawer open onOpenChange={onOpenChange} title="Criar incidente" closeLabel="Fechar">
        <p>conteúdo</p>
      </Drawer>
    );
    await userEvent.keyboard("{Escape}");
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("fechado não renderiza conteúdo", () => {
    render(
      <Drawer open={false} onOpenChange={() => {}} title="Criar status page" closeLabel="Fechar">
        <p>corpo</p>
      </Drawer>
    );
    expect(screen.queryByText("corpo")).not.toBeInTheDocument();
  });

  it("sempre renderiza o X de fechar (mesmo modelo de chrome em todos os drawers)", async () => {
    const onOpenChange = vi.fn();
    render(
      <Drawer open onOpenChange={onOpenChange} title="Criar status page" closeLabel="Fechar">
        <p>corpo</p>
      </Drawer>
    );
    await userEvent.click(screen.getByRole("button", { name: "Fechar" }));
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });
});
