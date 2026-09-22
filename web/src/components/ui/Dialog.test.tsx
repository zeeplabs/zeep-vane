import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Dialog } from "./Dialog";

describe("Dialog", () => {
  it("renders title and content when open", () => {
    render(
      <Dialog open onOpenChange={() => {}} title="Sair do painel">
        <p>corpo</p>
      </Dialog>
    );
    expect(screen.getByText("Sair do painel")).toBeInTheDocument();
    expect(screen.getByText("corpo")).toBeInTheDocument();
  });

  it("with disableBackdropDismiss, Escape does not close the dialog", async () => {
    const onOpenChange = vi.fn();
    render(
      <Dialog open onOpenChange={onOpenChange} title="Sessão expirada" disableBackdropDismiss>
        <p>bloqueante</p>
      </Dialog>
    );
    await userEvent.keyboard("{Escape}");
    expect(onOpenChange).not.toHaveBeenCalled();
  });

  it("without disableBackdropDismiss, Escape calls onOpenChange(false)", async () => {
    const onOpenChange = vi.fn();
    render(
      <Dialog open onOpenChange={onOpenChange} title="Confirmar">
        <p>conteúdo</p>
      </Dialog>
    );
    await userEvent.keyboard("{Escape}");
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("footer is right-aligned and separated from the content by a border", () => {
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
