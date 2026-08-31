import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Dialog } from "./Dialog";

describe("Dialog", () => {
  it("renderiza título e conteúdo quando aberto", () => {
    render(
      <Dialog open onOpenChange={() => {}} title="Sair do painel">
        <p>corpo</p>
      </Dialog>
    );
    expect(screen.getByText("Sair do painel")).toBeInTheDocument();
    expect(screen.getByText("corpo")).toBeInTheDocument();
  });

  it("com disableBackdropDismiss, Escape não fecha o dialog", async () => {
    const onOpenChange = vi.fn();
    render(
      <Dialog open onOpenChange={onOpenChange} title="Sessão expirada" disableBackdropDismiss>
        <p>bloqueante</p>
      </Dialog>
    );
    await userEvent.keyboard("{Escape}");
    expect(onOpenChange).not.toHaveBeenCalled();
  });

  it("sem disableBackdropDismiss, Escape chama onOpenChange(false)", async () => {
    const onOpenChange = vi.fn();
    render(
      <Dialog open onOpenChange={onOpenChange} title="Confirmar">
        <p>conteúdo</p>
      </Dialog>
    );
    await userEvent.keyboard("{Escape}");
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("footer fica alinhado à direita e separado do conteúdo por uma borda", () => {
    render(
      <Dialog
        open
        onOpenChange={() => {}}
        title="Excluir item"
        footer={
          <>
            <button type="button">Cancelar</button>
            <button type="button">Excluir</button>
          </>
        }
      >
        <p>Tem certeza?</p>
      </Dialog>
    );
    const footer = screen.getByRole("button", { name: "Cancelar" }).parentElement!;
    expect(footer.className).toContain("justify-end");
    expect(footer.className).toContain("border-t");
  });
});
