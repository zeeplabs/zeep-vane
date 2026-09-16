import { describe, it, expect, afterEach } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
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
describe("App - /services route", () => {
  it("authenticated loading /services renders the ServiceListPage inside the shell", async () => {
    setBootstrapped(true);
    await loginAs("owner@vane.app");
    renderAppAt("/services");

    await waitFor(() =>
      expect(screen.getByRole("heading", { level: 1, name: "Serviços monitorados" })).toBeInTheDocument()
    );
    // The sidebar navigation (AuthenticatedLayout/shell) remains present -
    // proof that the page is inside the shell, not an isolated render (L-050).
    expect(screen.getByText("Não configurado")).toBeInTheDocument();
  });

  it("without a session redirects to /login", async () => {
    setBootstrapped(true);
    renderAppAt("/services");

    await waitFor(() => expect(screen.getByRole("heading", { name: "Entrar" })).toBeInTheDocument());
    expect(
      screen.queryByRole("heading", { level: 1, name: "Serviços monitorados" })
    ).not.toBeInTheDocument();
  });

  it("viewer does not see the 'Adicionar serviço' button", async () => {
    setBootstrapped(true);
    await loginAs("viewer@vane.app");
    renderAppAt("/services");

    await waitFor(() =>
      expect(screen.getByRole("heading", { level: 1, name: "Serviços monitorados" })).toBeInTheDocument()
    );
    expect(screen.queryByRole("button", { name: "Adicionar serviço" })).not.toBeInTheDocument();
  });

  it("clicking a row opens that service's ServiceDetailDrawer", async () => {
    setBootstrapped(true);
    await loginAs("owner@vane.app");
    renderAppAt("/services");
    await screen.findByText("Notificações");

    await userEvent.click(screen.getByText("Notificações"));

    expect(await screen.findByRole("button", { name: "Fechar" })).toBeInTheDocument();
  });

  it("'Adicionar serviço' opens the AddServiceDrawer and a successful registration appears in the list without reload", async () => {
    setBootstrapped(true);
    await loginAs("owner@vane.app");
    renderAppAt("/services");
    await screen.findByText("Notificações");

    await userEvent.click(screen.getByRole("button", { name: "Adicionar serviço" }));
    const dialog = screen.getByRole("dialog");
    await userEvent.type(screen.getByLabelText("Nome do serviço"), "Fila de pagamentos");
    await userEvent.type(screen.getByLabelText("Buscar SLO"), "checkout");
    const option = await screen.findByRole("button", { name: /Checkout/i });
    await userEvent.click(option);
    await userEvent.click(within(dialog).getByRole("button", { name: "Adicionar serviço" }));

    await waitFor(() =>
      expect(screen.queryByLabelText("Nome do serviço")).not.toBeInTheDocument()
    );
    expect(await screen.findByText("Fila de pagamentos")).toBeInTheDocument();
  });
});
