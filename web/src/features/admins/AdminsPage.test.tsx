import { describe, it, expect, afterEach } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { Toaster } from "sonner";
import "../../lib/i18n";
import { AuthProvider } from "../../auth/AuthProvider";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch } from "../../lib/apiClient";
import { seedExpiredAdminInvite } from "../../test/msw/handlers";
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
          <Toaster />
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

  it("alterar papel com sucesso atualiza o papel exibido na lista (AF-27)", async () => {
    await loginAsOwner();
    renderPage();
    await screen.findByText("operator@vane.app");

    const operatorRow = screen.getByText("operator@vane.app").closest("tr")!;
    const viewerIcon = within(operatorRow).getByRole("button", { name: "Viewer" });
    await userEvent.click(viewerIcon);
    await userEvent.click(screen.getByRole("button", { name: "Confirmar" }));

    await waitFor(() => {
      const updatedRow = screen.getByText("operator@vane.app").closest("tr")!;
      expect(within(updatedRow).getByRole("button", { name: "Viewer" })).toHaveAttribute(
        "aria-pressed",
        "true"
      );
    });
  });

  it("remover admin com sucesso remove a linha e exibe toast de confirmação (AF-28)", async () => {
    await loginAsOwner();
    renderPage();
    await screen.findByText("operator@vane.app");

    const operatorRow = screen.getByText("operator@vane.app").closest("tr")!;
    await userEvent.click(within(operatorRow).getByRole("button", { name: "Remover" }));
    await screen.findByText("Remover admin");
    await userEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Remover" }));

    await waitFor(() => expect(screen.queryByText("operator@vane.app")).not.toBeInTheDocument());
    expect(await screen.findByText("Acesso de operator@vane.app removido.")).toBeInTheDocument();
  });

  it("convidar admin com sucesso adiciona a linha com badge Pendente (AF-25)", async () => {
    await loginAsOwner();
    renderPage();
    await screen.findByText("owner@vane.app");

    await userEvent.click(screen.getByRole("button", { name: "Convidar admin" }));
    await userEvent.type(screen.getByLabelText("E-mail"), "novo-viewer@vane.app");
    await userEvent.selectOptions(screen.getByLabelText("Papel"), "viewer");
    await userEvent.click(screen.getByRole("button", { name: "Enviar convite" }));

    expect(await screen.findByText("novo-viewer@vane.app")).toBeInTheDocument();
    const newRow = screen.getByText("novo-viewer@vane.app").closest("tr")!;
    expect(within(newRow).getByText("Pendente")).toBeInTheDocument();
  });

  it("reenviar convite pendente mantém a linha e exibe toast de confirmação (INVITE-03)", async () => {
    await loginAsOwner();
    renderPage();
    await screen.findByText("novo-operador@vane.app");

    const inviteRow = screen.getByText("novo-operador@vane.app").closest("tr")!;
    await userEvent.click(within(inviteRow).getByRole("button", { name: "Reenviar" }));

    expect(await screen.findByText("Convite reenviado para novo-operador@vane.app.")).toBeInTheDocument();
    expect(screen.getByText("novo-operador@vane.app")).toBeInTheDocument();
  });

  it("cancelar convite pendente remove a linha e exibe toast de confirmação (INVITE-05)", async () => {
    await loginAsOwner();
    renderPage();
    await screen.findByText("novo-operador@vane.app");

    const inviteRow = screen.getByText("novo-operador@vane.app").closest("tr")!;
    await userEvent.click(within(inviteRow).getByRole("button", { name: "Cancelar" }));

    await waitFor(() => expect(screen.queryByText("novo-operador@vane.app")).not.toBeInTheDocument());
    expect(await screen.findByText("Convite de novo-operador@vane.app cancelado.")).toBeInTheDocument();
  });

  it("convite pendente expirado exibe tag Expirado além de Pendente (INVITE-07)", async () => {
    await loginAsOwner();
    seedExpiredAdminInvite("expirado@vane.app", "viewer");
    renderPage();
    await screen.findByText("expirado@vane.app");

    const expiredRow = screen.getByText("expirado@vane.app").closest("tr")!;
    expect(within(expiredRow).getByText("Expirado")).toBeInTheDocument();
    expect(within(expiredRow).getByText("Pendente")).toBeInTheDocument();

    const freshRow = screen.getByText("novo-operador@vane.app").closest("tr")!;
    expect(within(freshRow).queryByText("Expirado")).not.toBeInTheDocument();
  });
});
