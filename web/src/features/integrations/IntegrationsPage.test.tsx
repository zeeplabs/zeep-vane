import { describe, it, expect, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import "../../lib/i18n";
import { AuthProvider } from "../../auth/AuthProvider";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch } from "../../lib/apiClient";
import { IntegrationsPage } from "./IntegrationsPage";

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
          <IntegrationsPage />
        </AuthProvider>
      </TestQueryProvider>
    </MemoryRouter>
  );
}

describe("IntegrationsPage", () => {
  it("mostra card conectado com chave mascarada e tag Conectado para owner", async () => {
    await loginAs("owner@vane.app");
    renderPage();
    expect(await screen.findByText("Datadog")).toBeInTheDocument();
    expect(screen.getByText("Conectado")).toBeInTheDocument();
    expect(screen.getByText(/Chave: •••• •••• •••• /)).toBeInTheDocument();
  });

  it("viewer não vê o botão de rotacionar chave", async () => {
    await loginAs("viewer@vane.app");
    renderPage();
    await screen.findByText("Datadog");
    expect(screen.queryByRole("button", { name: "Rotacionar chave" })).not.toBeInTheDocument();
  });

  it("rotacionar chave abre form e salva com sucesso", async () => {
    await loginAs("owner@vane.app");
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "Rotacionar chave" }));
    await userEvent.type(screen.getByLabelText("Nova API key"), "novachave1234");
    await userEvent.type(screen.getByLabelText("Nova App key"), "novaappkey");
    await userEvent.click(screen.getByRole("button", { name: "Salvar chave" }));

    await waitFor(() => expect(screen.queryByLabelText("Nova API key")).not.toBeInTheDocument());
    expect(await screen.findByText(/1234/)).toBeInTheDocument();
  });
});
