import { describe, it, expect, afterEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import "../../lib/i18n";
import { AuthProvider } from "../../auth/AuthProvider";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch } from "../../lib/apiClient";
import { pollerStatus } from "../../lib/mockData";
import { PollerStatusPage } from "./PollerStatusPage";

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

afterEach(async () => {
  pollerStatus[0].status = "active";
  pollerStatus[0].last_error = null;
  await apiFetch("/api/auth/logout", { method: "POST" });
});

function renderPage() {
  return render(
    <MemoryRouter>
      <TestQueryProvider>
        <AuthProvider>
          <PollerStatusPage />
        </AuthProvider>
      </TestQueryProvider>
    </MemoryRouter>
  );
}

describe("PollerStatusPage", () => {
  it("mostra tabela com Integração/Última execução/Resultado, mensagem de erro só em falha", async () => {
    await loginAsOwner();
    renderPage();
    expect(await screen.findByText("datadog")).toBeInTheDocument();
    expect(screen.getByText("Sucesso")).toBeInTheDocument();
    expect(screen.getByText("Integração")).toBeInTheDocument();
    expect(screen.getByText("Última execução")).toBeInTheDocument();
  });

  it("integração com falha mostra tag Falha e a mensagem de erro", async () => {
    pollerStatus[0].status = "invalid";
    pollerStatus[0].last_error = "Credenciais inválidas";
    await loginAsOwner();
    renderPage();

    expect(await screen.findByText("Falha")).toBeInTheDocument();
    expect(screen.getByText("Credenciais inválidas")).toBeInTheDocument();
  });
});
