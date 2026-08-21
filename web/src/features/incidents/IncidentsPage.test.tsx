import { describe, it, expect, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import "../../lib/i18n";
import { AuthProvider } from "../../auth/AuthProvider";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch } from "../../lib/apiClient";
import { IncidentsPage } from "./IncidentsPage";

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
          <IncidentsPage />
        </AuthProvider>
      </TestQueryProvider>
    </MemoryRouter>
  );
}

describe("IncidentsPage", () => {
  it("incidente não-resolvido aparece na aba Ativos, separado dos resolvidos", async () => {
    await loginAs("owner@vane.app");
    renderPage();
    expect(await screen.findByText("Latência elevada no Checkout")).toBeInTheDocument();
    expect(screen.queryByText("Indisponibilidade parcial da API")).not.toBeInTheDocument();
  });

  it("aba Resolvidos mostra o incidente resolvido com botão Reabrir para quem gerencia", async () => {
    await loginAs("owner@vane.app");
    renderPage();
    await screen.findByText("Latência elevada no Checkout");
    await userEvent.click(screen.getByRole("tab", { name: "Resolvidos" }));

    expect(await screen.findByText("Indisponibilidade parcial da API")).toBeInTheDocument();
    expect(screen.getByText("Resolvido")).toBeInTheDocument();
    expect(await screen.findByRole("button", { name: /Reabrir incidente/ })).toBeInTheDocument();
  });

  it("viewer não vê o formulário de criação nem o botão de reabrir", async () => {
    await loginAs("viewer@vane.app");
    renderPage();
    await screen.findByText("Latência elevada no Checkout");
    expect(screen.queryByRole("button", { name: "Novo incidente" })).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole("tab", { name: "Resolvidos" }));
    await screen.findByText("Indisponibilidade parcial da API");
    expect(screen.queryByRole("button", { name: /Reabrir incidente/ })).not.toBeInTheDocument();
  });

  it("criar incidente com serviços selecionados aparece na aba Ativos", async () => {
    await loginAs("owner@vane.app");
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "Novo incidente" }));
    await userEvent.type(screen.getByLabelText("Título"), "Falha de teste E2E");
    await userEvent.click(screen.getByRole("button", { name: "API pública" }));
    await userEvent.click(screen.getByRole("button", { name: "Criar" }));

    await waitFor(() => expect(screen.queryByLabelText("Título")).not.toBeInTheDocument());
    expect(await screen.findByText("Falha de teste E2E")).toBeInTheDocument();
  });
});
