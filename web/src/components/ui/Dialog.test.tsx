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
});
