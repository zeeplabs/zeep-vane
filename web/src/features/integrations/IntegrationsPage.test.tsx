import { describe, it, expect, afterEach } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
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
    </MemoryRouter>,
  );
}

async function cardOf(name: string) {
  const heading = await screen.findByText(name);
  return heading.closest(".rounded-2xl") as HTMLElement;
}

describe("IntegrationsPage", () => {
  it("agrupa as integrações nas 3 categorias do mock com contagem", async () => {
    await loginAs("owner@vane.app");
    renderPage();

    expect(await screen.findByText("APM & Observabilidade")).toBeInTheDocument();
    expect(screen.getAllByText("2 integrações")).toHaveLength(2); // APM (Datadog+New Relic) e E-mail (Resend+SendGrid)
    expect(screen.getByText("IA")).toBeInTheDocument();
    expect(screen.getByText("1 integração")).toBeInTheDocument();
    expect(screen.getByText("E-mail")).toBeInTheDocument();
  });

  it("mostra Datadog conectado (seed) e New Relic sempre como Em breve", async () => {
    await loginAs("owner@vane.app");
    renderPage();

    const datadogCard = await cardOf("Datadog");
    expect(await within(datadogCard).findByText(/^Sincronizado/)).toBeInTheDocument();
    expect(within(datadogCard).getByText("Conectado")).toBeInTheDocument();

    const newRelicCard = await cardOf("New Relic");
    expect(within(newRelicCard).getByText("Em breve")).toBeInTheDocument();
    expect(within(newRelicCard).queryByRole("button")).not.toBeInTheDocument();
  });

  it("LLM Provider e provedores de e-mail começam não conectados", async () => {
    await loginAs("owner@vane.app");
    renderPage();

    const llmCard = await cardOf("LLM Provider");
    expect(within(llmCard).getByText("Não conectado")).toBeInTheDocument();

    const resendCard = await cardOf("Resend");
    expect(within(resendCard).getByText("Não conectado")).toBeInTheDocument();

    const sendgridCard = await cardOf("SendGrid");
    expect(within(sendgridCard).getByText("Não conectado")).toBeInTheDocument();
  });

  it("viewer não vê nenhum botão de ação nos cards", async () => {
    await loginAs("viewer@vane.app");
    renderPage();

    await screen.findByText("Datadog");
    expect(screen.queryByRole("button", { name: "Editar conexão" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Conectar" })).not.toBeInTheDocument();
  });

  it("owner conecta Resend via drawer e o card atualiza pra Conectado", async () => {
    await loginAs("owner@vane.app");
    renderPage();

    const resendCard = await cardOf("Resend");
    await within(resendCard).findByText("Não conectado");
    await userEvent.click(within(resendCard).getByRole("button", { name: "Conectar" }));

    const drawerTitle = await screen.findByRole("heading", { name: "Conectar Resend" });
    const drawer = drawerTitle.closest('[role="dialog"]') as HTMLElement;
    await userEvent.type(within(drawer).getByLabelText("API key"), "re-real-key");
    await userEvent.type(within(drawer).getByLabelText("E-mail do remetente"), "no-reply@acme.example.com");
    await userEvent.type(within(drawer).getByLabelText("Nome do remetente"), "Acme");
    await userEvent.click(within(drawer).getByRole("button", { name: "Salvar" }));

    await waitFor(() => expect(screen.queryByRole("heading", { name: "Conectar Resend" })).not.toBeInTheDocument());
    const updatedCard = await cardOf("Resend");
    expect(within(updatedCard).getByText("Conectado")).toBeInTheDocument();
  });

  it("owner clica em Editar conexão no Datadog (já conectado) e abre o drawer de conectar novamente", async () => {
    await loginAs("owner@vane.app");
    renderPage();

    const datadogCard = await cardOf("Datadog");
    await within(datadogCard).findByText(/^Sincronizado/);
    await userEvent.click(within(datadogCard).getByRole("button", { name: "Editar conexão" }));

    expect(await screen.findByRole("heading", { name: "Conectar Datadog" })).toBeInTheDocument();
  });
});
