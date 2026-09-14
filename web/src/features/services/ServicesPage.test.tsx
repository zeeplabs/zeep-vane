import { describe, it, expect, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import "../../lib/i18n";
import App from "../../App";
import { setBootstrapped } from "../../test/msw/handlers";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch } from "../../lib/apiClient";

function renderAppAt(path: string) {
  return render(
    <TestQueryProvider>
      <MemoryRouter initialEntries={[path]}>
        <App />
      </MemoryRouter>
    </TestQueryProvider>
  );
}

async function loginAs(email: string) {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email, password: "demo1234" }),
  });
}

afterEach(async () => {
  await apiFetch("/api/auth/logout", { method: "POST" });
});

// T11: /services now renders ServiceListPage (through the authenticated
// shell route, not a bare component render - L-050) instead of
// ServicesSection.
describe("App - rota /services", () => {
  it("autenticado carregando /services renderiza a ServiceListPage dentro do shell", async () => {
    setBootstrapped(true);
    await loginAs("owner@vane.app");
    renderAppAt("/services");

    await waitFor(() =>
      expect(screen.getByRole("heading", { level: 1, name: "Serviços monitorados" })).toBeInTheDocument()
    );
    // A navegação lateral (AuthenticatedLayout/shell) segue presente -
    // prova que a página está dentro do shell, não um render isolado (L-050).
    expect(screen.getByText("Não configurado")).toBeInTheDocument();
  });

  it("sem sessão redireciona para /login", async () => {
    setBootstrapped(true);
    renderAppAt("/services");

    await waitFor(() => expect(screen.getByRole("heading", { name: "Entrar" })).toBeInTheDocument());
    expect(
      screen.queryByRole("heading", { level: 1, name: "Serviços monitorados" })
    ).not.toBeInTheDocument();
  });

  it("viewer não vê o botão 'Adicionar serviço'", async () => {
    setBootstrapped(true);
    await loginAs("viewer@vane.app");
    renderAppAt("/services");

    await waitFor(() =>
      expect(screen.getByRole("heading", { level: 1, name: "Serviços monitorados" })).toBeInTheDocument()
    );
    expect(screen.queryByRole("button", { name: "Adicionar serviço" })).not.toBeInTheDocument();
  });

  it("clicar numa linha abre o ServiceDetailDrawer daquele serviço", async () => {
    setBootstrapped(true);
    await loginAs("owner@vane.app");
    renderAppAt("/services");
    await screen.findByText("Notificações");

    await userEvent.click(screen.getByText("Notificações"));

    expect(await screen.findByRole("button", { name: "Fechar" })).toBeInTheDocument();
  });

  it("'Adicionar serviço' abre o AddServiceDrawer e um cadastro bem-sucedido aparece na lista sem reload", async () => {
    setBootstrapped(true);
    await loginAs("owner@vane.app");
    renderAppAt("/services");
    await screen.findByText("Notificações");

    await userEvent.click(screen.getByRole("button", { name: "Adicionar serviço" }));
    await userEvent.type(screen.getByLabelText("Nome do serviço"), "Fila de pagamentos");
    await userEvent.type(screen.getByLabelText("Buscar SLO"), "checkout");
    const option = await screen.findByRole("button", { name: /Checkout/i });
    await userEvent.click(option);
    await userEvent.click(screen.getByRole("button", { name: "Salvar" }));

    await waitFor(() =>
      expect(screen.queryByLabelText("Nome do serviço")).not.toBeInTheDocument()
    );
    expect(await screen.findByText("Fila de pagamentos")).toBeInTheDocument();
  });
});
