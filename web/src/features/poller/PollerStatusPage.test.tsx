import { describe, it, expect, afterEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { http, HttpResponse } from "msw";
import "../../lib/i18n";
import { AuthProvider } from "../../auth/AuthProvider";
import { TestQueryProvider } from "../../test/queryClient";
import { server } from "../../test/msw/server";
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
    expect(await screen.findByText("Datadog")).toBeInTheDocument();
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

  it("renderiza Pager com Página 1 de 1 (fixture cabe em uma página)", async () => {
    await loginAsOwner();
    renderPage();

    await screen.findByText("Datadog");
    expect(screen.getByText("Página 1 de 1")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Anterior" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Próximo" })).toBeDisabled();
  });

  it("clicar em Próximo busca a próxima página com o provider correto", async () => {
    server.use(
      http.get("/api/poller/status", ({ request }) => {
        const page = new URL(request.url).searchParams.get("page") === "2" ? 2 : 1;
        return HttpResponse.json({
          items: [
            {
              provider: page === 1 ? "datadog" : "sendgrid",
              status: "active",
              last_checked_at: new Date().toISOString(),
              last_error: null,
            },
          ],
          total: 21,
          page,
          page_size: 20,
        });
      })
    );
    await loginAsOwner();
    renderPage();

    await screen.findByText("Datadog");
    await userEvent.click(screen.getByRole("button", { name: "Próximo" }));

    await screen.findByText("Sendgrid");
    expect(screen.getByText("Página 2 de 2")).toBeInTheDocument();
  });
});
