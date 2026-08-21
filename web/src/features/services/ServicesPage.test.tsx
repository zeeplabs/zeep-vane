import { describe, it, expect, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import "../../lib/i18n";
import { AuthProvider } from "../../auth/AuthProvider";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch } from "../../lib/apiClient";
import { ServicesPage } from "./ServicesPage";

async function loginAs(email: string) {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email, password: "demo1234" }),
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
          <ServicesPage />
        </AuthProvider>
      </TestQueryProvider>
    </MemoryRouter>
  );
}

describe("ServicesPage", () => {
  it("lista serviços com colunas Serviço/SLO vinculado/Status/Última mudança", async () => {
    await loginAs("owner@vane.app");
    renderPage();
    expect(await screen.findByText("Notificações")).toBeInTheDocument();
    expect(screen.getByText("Serviço")).toBeInTheDocument();
    expect(screen.getByText("SLO vinculado")).toBeInTheDocument();
    expect(screen.getByText("Não configurado")).toBeInTheDocument();
  });

  it("viewer não vê o botão Vincular serviço", async () => {
    await loginAs("viewer@vane.app");
    renderPage();
    await screen.findByText("Notificações");
    expect(screen.queryByRole("button", { name: "Vincular serviço" })).not.toBeInTheDocument();
  });

  it("selecionar um SLO da busca e criar o serviço remove o rótulo not configured", async () => {
    await loginAs("owner@vane.app");
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "Vincular serviço" }));
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
