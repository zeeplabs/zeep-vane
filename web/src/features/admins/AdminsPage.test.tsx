import { describe, it, expect, afterEach } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import "../../lib/i18n";
import { AuthProvider } from "../../auth/AuthProvider";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch } from "../../lib/apiClient";
import { AdminsPage } from "./AdminsPage";

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

afterEach(async () => {
  await apiFetch("/api/auth/logout", { method: "POST" });
});

function renderPage() {
  return render(
    <MemoryRouter>
      <TestQueryProvider>
        <AuthProvider>
          <AdminsPage />
        </AuthProvider>
      </TestQueryProvider>
    </MemoryRouter>
  );
}

describe("AdminsPage", () => {
  it("convite pendente aparece com badge Pendente", async () => {
    await loginAsOwner();
    renderPage();
    expect(await screen.findByText("novo-operador@vane.app")).toBeInTheDocument();
    expect(screen.getByText("Pendente")).toBeInTheDocument();
  });

  it("coluna Papel usa IconRoleSelector, não um <select>", async () => {
    await loginAsOwner();
    renderPage();
    await screen.findByText("owner@vane.app");
    expect(screen.getAllByRole("group", { name: "Selecionar papel" }).length).toBeGreaterThan(0);
    expect(screen.queryAllByRole("combobox")).toHaveLength(0);
  });

  it("rejeição 409 (lockout de último owner) exibe erro e mantém a linha anterior", async () => {
    await loginAsOwner();
    renderPage();
    await screen.findByText("owner@vane.app");

    const ownerRow = screen.getByText("owner@vane.app").closest("tr")!;
    const operatorIcon = ownerRow.querySelector('button[aria-label="Operator"]') as HTMLElement;
    await userEvent.click(operatorIcon);

    await userEvent.click(screen.getByRole("button", { name: "Confirmar" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(/zero active owners/);
    expect(screen.getByText("owner@vane.app")).toBeInTheDocument();
  });

  it("remover admin abre confirmação com copy exato", async () => {
    await loginAsOwner();
    renderPage();
    await screen.findByText("operator@vane.app");

    const operatorRow = screen.getByText("operator@vane.app").closest("tr")!;
    await userEvent.click(within(operatorRow).getByRole("button", { name: "Remover" }));

    expect(await screen.findByText("Remover admin")).toBeInTheDocument();
    expect(
      screen.getByText("Remover o acesso de operator@vane.app? Esta ação não pode ser desfeita.")
    ).toBeInTheDocument();
  });

  it("convites pendentes tem ações Reenviar/Cancelar desabilitadas (sem backend, backlog AD-007)", async () => {
    await loginAsOwner();
    renderPage();
    await screen.findByText("novo-operador@vane.app");

    const resendButton = screen.getByRole("button", { name: "Reenviar" });
    const cancelButton = screen.getByRole("button", { name: "Cancelar" });
    expect(resendButton).toBeDisabled();
    expect(cancelButton).toBeDisabled();
  });

  it("clicar em Reenviar/Cancelar desabilitados não dispara requisição nem remove a linha", async () => {
    await loginAsOwner();
    renderPage();
    await screen.findByText("novo-operador@vane.app");

    await userEvent.click(screen.getByRole("button", { name: "Reenviar" }));
    await userEvent.click(screen.getByRole("button", { name: "Cancelar" }));

    expect(screen.getByText("novo-operador@vane.app")).toBeInTheDocument();
  });
});
