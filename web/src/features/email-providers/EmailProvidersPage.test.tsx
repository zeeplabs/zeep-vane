import { describe, it, expect, afterEach } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { http, HttpResponse } from "msw";
import "../../lib/i18n";
import { AuthProvider } from "../../auth/AuthProvider";
import { TestQueryProvider } from "../../test/queryClient";
import { server } from "../../test/msw/server";
import { apiFetch } from "../../lib/apiClient";
import { EmailProvidersPage } from "./EmailProvidersPage";

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
          <EmailProvidersPage />
        </AuthProvider>
      </TestQueryProvider>
    </MemoryRouter>
  );
}

async function providerCard(name: string) {
  const heading = await screen.findByText(name);
  return heading.closest(".flex.flex-col") as HTMLElement;
}

describe("EmailProvidersPage", () => {
  it("mostra SendGrid e Resend como não conectados quando nada foi configurado", async () => {
    await loginAs("owner@vane.app");
    renderPage();

    const sendgridCard = await providerCard("SendGrid");
    const resendCard = await providerCard("Resend");
    expect(within(sendgridCard).getByText("Não conectado")).toBeInTheDocument();
    expect(within(resendCard).getByText("Não conectado")).toBeInTheDocument();
  });

  it("viewer não vê botões de conectar/ativar", async () => {
    await loginAs("viewer@vane.app");
    renderPage();

    await screen.findByText("SendGrid");
    expect(screen.queryByRole("button", { name: "Conectar" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Ativar" })).not.toBeInTheDocument();
  });

  it("conectar um provider mostra erro inline em 422 e depois conecta com sucesso", async () => {
    await loginAs("owner@vane.app");
    renderPage();

    const card = await providerCard("SendGrid");
    await userEvent.click(within(card).getByRole("button", { name: "Conectar" }));
    await userEvent.type(within(card).getByLabelText("SendGrid API key"), "invalid-key");
    await userEvent.type(within(card).getByLabelText("E-mail do remetente"), "no-reply@acme.example.com");
    await userEvent.type(within(card).getByLabelText("Nome do remetente"), "Acme");
    await userEvent.click(within(card).getByRole("button", { name: "Salvar" }));

    expect(await within(card).findByRole("alert")).toHaveTextContent(
      /invalid email provider api key, from_email, or from_name/
    );

    // O form permanece aberto após o erro - corrige a chave e salva de novo.
    await userEvent.clear(within(card).getByLabelText("SendGrid API key"));
    await userEvent.type(within(card).getByLabelText("SendGrid API key"), "sg-real-key");
    await userEvent.click(within(card).getByRole("button", { name: "Salvar" }));

    await waitFor(() => expect(within(card).getByText("Conectado")).toBeInTheDocument());
    expect(within(card).queryByLabelText("SendGrid API key")).not.toBeInTheDocument();
  });

  it("ativar um provider conectado atualiza o estado exibido sem reload", async () => {
    await loginAs("owner@vane.app");
    // Seeds a connected-but-inactive resend row through the real connect
    // flow (not a server.use() override), so the fixture's in-memory state
    // stays consistent for the activate call the test drives next.
    await apiFetch("/api/integrations/email/resend", {
      method: "POST",
      body: JSON.stringify({ api_key: "re-real-key", from_email: "a@b.com", from_name: "A" }),
    });
    renderPage();

    const card = await providerCard("Resend");
    expect(await within(card).findByText("Conectado")).toBeInTheDocument();
    await userEvent.click(within(card).getByRole("button", { name: "Ativar" }));

    await waitFor(() => expect(within(card).getByText("Ativo")).toBeInTheDocument());
    expect(within(card).queryByRole("button", { name: "Ativar" })).not.toBeInTheDocument();
  });

  it("mostra last_error para um provider com status invalid", async () => {
    await loginAs("owner@vane.app");
    server.use(
      http.get("/api/integrations/email", () =>
        HttpResponse.json({
          active_provider: null,
          providers: [
            {
              provider: "sendgrid",
              status: "invalid",
              from_email: "a@b.com",
              from_name: "A",
              last_checked_at: "2026-01-01T00:00:00Z",
              last_error: "chave revogada",
            },
          ],
        })
      )
    );
    renderPage();

    const card = await providerCard("SendGrid");
    expect(await within(card).findByText("Inválido")).toBeInTheDocument();
    expect(within(card).getByText(/chave revogada/)).toBeInTheDocument();
  });
});
