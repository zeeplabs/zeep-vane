import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Drawer } from "./Drawer";

describe("Drawer", () => {
  it("renders title, description, content and footer when open", () => {
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

  it("Escape calls onOpenChange(false)", async () => {
    const onOpenChange = vi.fn();
    render(
      <Drawer open onOpenChange={onOpenChange} title="Criar incidente" closeLabel="Fechar">
        <p>conteúdo</p>
      </Drawer>
    );
    await userEvent.keyboard("{Escape}");
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("closed does not render content", () => {
    render(
      <Drawer open={false} onOpenChange={() => {}} title="Criar status page" closeLabel="Fechar">
        <p>corpo</p>
      </Drawer>
    );
    expect(screen.queryByText("corpo")).not.toBeInTheDocument();
  });

  it("always renders the close X (same chrome model across all drawers)", async () => {
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
