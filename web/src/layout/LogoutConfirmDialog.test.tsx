import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "../lib/i18n";
import { LogoutConfirmDialog } from "./LogoutConfirmDialog";

describe("LogoutConfirmDialog", () => {
  it("renders the same title/copy as the logout modal", () => {
    render(<LogoutConfirmDialog open onOpenChange={() => {}} onConfirm={() => {}} />);
    expect(screen.getByText("Sair do painel")).toBeInTheDocument();
    expect(screen.getByText("Tem certeza que deseja encerrar sua sessão?")).toBeInTheDocument();
  });

  it("confirming calls onConfirm", async () => {
    const onConfirm = vi.fn();
    render(<LogoutConfirmDialog open onOpenChange={() => {}} onConfirm={onConfirm} />);
    await userEvent.click(screen.getByRole("button", { name: "Sair" }));
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });

  it("canceling closes without calling onConfirm", async () => {
    const onConfirm = vi.fn();
    const onOpenChange = vi.fn();
    render(<LogoutConfirmDialog open onOpenChange={onOpenChange} onConfirm={onConfirm} />);
    await userEvent.click(screen.getByRole("button", { name: "Cancelar" }));
    expect(onOpenChange).toHaveBeenCalledWith(false);
    expect(onConfirm).not.toHaveBeenCalled();
  });
});
