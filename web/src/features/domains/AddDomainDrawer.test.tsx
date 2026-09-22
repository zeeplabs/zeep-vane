import { describe, it, expect, afterEach } from "vitest";
import "../../lib/i18n";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch } from "../../lib/apiClient";
import { AddDomainDrawer } from "./AddDomainDrawer";

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

afterEach(async () => {
  await apiFetch("/api/auth/logout", { method: "POST" });
});

function renderDrawer(onOpenChange: (open: boolean) => void = () => {}) {
  return render(
    <TestQueryProvider>
      <AddDomainDrawer open onOpenChange={onOpenChange} />
    </TestQueryProvider>
  );
}

describe("AddDomainDrawer", () => {
  it("mostra as duas opções de tipo, com 'Domínio próprio' selecionado por padrão (DSP-09)", async () => {
    await loginAsOwner();
    renderDrawer();

    const customTile = screen.getByRole("button", { name: "Domínio próprio" });
    const vaneTile = screen.getByRole("button", { name: "Subdomínio Vane" });
    expect(customTile).toHaveAttribute("aria-pressed", "true");
    expect(vaneTile).toHaveAttribute("aria-pressed", "false");
  });

  it("o tile 'Subdomínio Vane' é desabilitado, sem onClick, e clicar nele não muda a seleção (DSP-10)", async () => {
    await loginAsOwner();
    renderDrawer();

    const vaneTile = screen.getByRole("button", { name: "Subdomínio Vane" });
    expect(vaneTile).toHaveAttribute("aria-disabled", "true");

    await userEvent.click(vaneTile);

    // Selection unchanged: "Domínio próprio" still the pressed tile, and
    // the hostname field (only rendered while "custom" is selected) is
    // still present - proving the click had no effect on selection.
    expect(screen.getByRole("button", { name: "Domínio próprio" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByLabelText("Hostname")).toBeInTheDocument();
  });

  it("envia o hostname digitado via POST /api/domains quando 'Domínio próprio' está selecionado (DSP-11)", async () => {
    await loginAsOwner();
    renderDrawer();

    await userEvent.type(screen.getByLabelText("Hostname"), "status.novo-dominio-teste.com");
    await userEvent.click(screen.getByRole("button", { name: "Adicionar domínio" }));

    await waitFor(async () => {
      const list = await apiFetch<{ items: { hostname: string }[] }>("/api/domains?page=1");
      expect(list.items.some((d) => d.hostname === "status.novo-dominio-teste.com")).toBe(true);
    });
  });

  it("hostname duplicado (409) mostra erro inline", async () => {
    await loginAsOwner();
    await apiFetch("/api/domains", {
      method: "POST",
      body: JSON.stringify({ hostname: "status.ja-existe.com" }),
    });
    renderDrawer();

    await userEvent.type(screen.getByLabelText("Hostname"), "status.ja-existe.com");
    await userEvent.click(screen.getByRole("button", { name: "Adicionar domínio" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("hostname already registered");
  });
});
