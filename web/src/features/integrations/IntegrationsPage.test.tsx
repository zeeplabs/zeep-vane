import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { http, HttpResponse } from "msw";
import i18n from "../../lib/i18n";
import { AuthProvider } from "../../auth/AuthProvider";
import { TestQueryProvider } from "../../test/queryClient";
import { server } from "../../test/msw/server";
import { apiFetch } from "../../lib/apiClient";
import { resetDeploymentMode, setDeploymentMode } from "../../test/msw/handlers";
import { IntegrationsPage } from "./IntegrationsPage";

async function loginAs(email: string) {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email, password: "demo1234" }),
  });
}

// Every email-provider assertion in this file exercises the self_hosted
// path (AD-034, SAASMAIL-11 AC3): in saas mode the email category doesn't
// render at all (see the dedicated saas-mode test below).
beforeEach(() => {
  setDeploymentMode("self_hosted");
});

afterEach(async () => {
  resetDeploymentMode();
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
  it("groups the integrations into the mock's 3 categories with a count", async () => {
    await loginAs("owner@vane.app");
    renderPage();

    expect(await screen.findByText("APM & Observabilidade")).toBeInTheDocument();
    expect(screen.getAllByText("2 integrações")).toHaveLength(2); // APM (Datadog+New Relic) and Email (Resend+SendGrid)
    expect(screen.getByText("IA")).toBeInTheDocument();
    expect(screen.getByText("1 integração")).toBeInTheDocument();
    expect(screen.getByText("E-mail")).toBeInTheDocument();
  });

  it("renders in English when the active language is en", async () => {
    await loginAs("owner@vane.app");
    await i18n.changeLanguage("en");

    try {
      renderPage();

      expect(await screen.findByText("Integrations")).toBeInTheDocument();
      expect(screen.getByText("APM & Observability")).toBeInTheDocument();
      expect(screen.getByText("Email")).toBeInTheDocument();
    } finally {
      await i18n.changeLanguage("pt-BR");
    }
  });

  it("renders the root-cause enrichment toggle label in English when the active language is en", async () => {
    await loginAs("owner@vane.app");
    await apiFetch("/api/integrations/llm/openai", {
      method: "POST",
      body: JSON.stringify({ api_key: "sk-real-key", model: "gpt-4o" }),
    });
    await apiFetch("/api/integrations/llm/openai/activate", { method: "POST" });
    await i18n.changeLanguage("en");

    try {
      renderPage();

      const llmCard = await cardOf("LLM Provider");
      expect(await within(llmCard).findByText("Root-cause enrichment")).toBeInTheDocument();
    } finally {
      await i18n.changeLanguage("pt-BR");
    }
  });

  it("shows Datadog connected (seed) and New Relic always as Coming soon", async () => {
    await loginAs("owner@vane.app");
    renderPage();

    const datadogCard = await cardOf("Datadog");
    expect(await within(datadogCard).findByText(/^Sincronizado/)).toBeInTheDocument();
    expect(within(datadogCard).getByText("Conectado")).toBeInTheDocument();

    const newRelicCard = await cardOf("New Relic");
    expect(within(newRelicCard).getByText("Em breve")).toBeInTheDocument();
    expect(within(newRelicCard).queryByRole("button")).not.toBeInTheDocument();
  });

  it("LLM Provider and email providers start out not connected", async () => {
    await loginAs("owner@vane.app");
    renderPage();

    const llmCard = await cardOf("LLM Provider");
    expect(within(llmCard).getByText("Não conectado")).toBeInTheDocument();

    const resendCard = await cardOf("Resend");
    expect(within(resendCard).getByText("Não conectado")).toBeInTheDocument();

    const sendgridCard = await cardOf("SendGrid");
    expect(within(sendgridCard).getByText("Não conectado")).toBeInTheDocument();
  });

  it("viewer sees no action buttons on the cards", async () => {
    await loginAs("viewer@vane.app");
    renderPage();

    await screen.findByText("Datadog");
    expect(screen.queryByRole("button", { name: "Editar conexão" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Conectar" })).not.toBeInTheDocument();
  });

  it("owner connects Resend via the drawer and the card updates to Connected", async () => {
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

  it("owner clicks Edit connection on Datadog (already connected) and reopens the connect drawer", async () => {
    await loginAs("owner@vane.app");
    renderPage();

    const datadogCard = await cardOf("Datadog");
    await within(datadogCard).findByText(/^Sincronizado/);
    await userEvent.click(within(datadogCard).getByRole("button", { name: "Editar conexão" }));

    expect(await screen.findByRole("heading", { name: "Conectar Datadog" })).toBeInTheDocument();
  });

  it("Datadog never connected (404) shows Not connected and a Connect button", async () => {
    server.use(
      http.get("/api/integrations/datadog/status", () =>
        HttpResponse.json({ error: "datadog integration not connected yet" }, { status: 404 }),
      ),
    );
    await loginAs("owner@vane.app");
    renderPage();

    const datadogCard = await cardOf("Datadog");
    expect(await within(datadogCard).findByText("Não conectado")).toBeInTheDocument();
    expect(within(datadogCard).getByText("Não configurado")).toBeInTheDocument();
    expect(within(datadogCard).getByRole("button", { name: "Conectar" })).toBeInTheDocument();
  });

  it("connected LLM Provider shows Connected and the active model", async () => {
    await loginAs("owner@vane.app");
    await apiFetch("/api/integrations/llm/openai", {
      method: "POST",
      body: JSON.stringify({ api_key: "sk-real-key", model: "gpt-4o" }),
    });
    renderPage();

    const llmCard = await cardOf("LLM Provider");
    expect(await within(llmCard).findByText("Conectado")).toBeInTheDocument();
    expect(within(llmCard).getByText("OpenAI · gpt-4o")).toBeInTheDocument();
    expect(within(llmCard).getByRole("button", { name: "Editar conexão" })).toBeInTheDocument();
  });

  it("active LLM Provider shows meta prefixed with 'Ativo' (INTGCARD-02)", async () => {
    await loginAs("owner@vane.app");
    await apiFetch("/api/integrations/llm/openai", {
      method: "POST",
      body: JSON.stringify({ api_key: "sk-real-key", model: "gpt-4o" }),
    });
    await apiFetch("/api/integrations/llm/openai/activate", { method: "POST" });
    renderPage();

    const llmCard = await cardOf("LLM Provider");
    expect(await within(llmCard).findByText("Ativo · OpenAI · gpt-4o")).toBeInTheDocument();
  });

  // TestIntegrationsPage covers slo-root-cause-enrichment RCA-07/RCA-08:
  // the toggle is hidden entirely - not shown, not greyed - unless an LLM
  // provider is currently active (design.md's resolved Risk).
  it("root-cause enrichment toggle is hidden when no LLM provider is connected", async () => {
    await loginAs("owner@vane.app");
    renderPage();

    const llmCard = await cardOf("LLM Provider");
    await within(llmCard).findByText("Não conectado");
    expect(within(llmCard).queryByRole("switch")).not.toBeInTheDocument();
  });

  it("root-cause enrichment toggle is hidden when the LLM provider is connected but not active", async () => {
    await loginAs("owner@vane.app");
    await apiFetch("/api/integrations/llm/openai", {
      method: "POST",
      body: JSON.stringify({ api_key: "sk-real-key", model: "gpt-4o" }),
    });
    renderPage();

    const llmCard = await cardOf("LLM Provider");
    await within(llmCard).findByText("OpenAI · gpt-4o");
    expect(within(llmCard).queryByRole("switch")).not.toBeInTheDocument();
  });

  it("root-cause enrichment toggle renders reflecting the current setting when the LLM provider is active", async () => {
    await loginAs("owner@vane.app");
    await apiFetch("/api/integrations/llm/openai", {
      method: "POST",
      body: JSON.stringify({ api_key: "sk-real-key", model: "gpt-4o" }),
    });
    await apiFetch("/api/integrations/llm/openai/activate", { method: "POST" });
    renderPage();

    const llmCard = await cardOf("LLM Provider");
    await within(llmCard).findByText("Ativo · OpenAI · gpt-4o");
    const toggle = within(llmCard).getByRole("switch");
    expect(toggle).toHaveAttribute("aria-checked", "false");
  });

  it("switching the root-cause enrichment toggle calls the PATCH endpoint and reflects the new state", async () => {
    await loginAs("owner@vane.app");
    await apiFetch("/api/integrations/llm/openai", {
      method: "POST",
      body: JSON.stringify({ api_key: "sk-real-key", model: "gpt-4o" }),
    });
    await apiFetch("/api/integrations/llm/openai/activate", { method: "POST" });
    renderPage();

    const llmCard = await cardOf("LLM Provider");
    await within(llmCard).findByText("Ativo · OpenAI · gpt-4o");
    const toggle = within(llmCard).getByRole("switch");
    await userEvent.click(toggle);

    await waitFor(() => expect(within(llmCard).getByRole("switch")).toHaveAttribute("aria-checked", "true"));
  });

  it("owner activates a connected-but-inactive LLM Provider by clicking Ativar (INTGCARD-01)", async () => {
    await loginAs("owner@vane.app");
    await apiFetch("/api/integrations/llm/openai", {
      method: "POST",
      body: JSON.stringify({ api_key: "sk-real-key", model: "gpt-4o" }),
    });
    renderPage();

    const llmCard = await cardOf("LLM Provider");
    await within(llmCard).findByText("OpenAI · gpt-4o");
    await userEvent.click(within(llmCard).getByRole("button", { name: "Ativar" }));

    await waitFor(async () => expect(await within(llmCard).findByText(/^Ativo/)).toBeInTheDocument());
  });

  it("owner confirms the LLM Provider disconnect dialog - card goes back to Not connected (INTGCARD-04)", async () => {
    await loginAs("owner@vane.app");
    await apiFetch("/api/integrations/llm/openai", {
      method: "POST",
      body: JSON.stringify({ api_key: "sk-real-key", model: "gpt-4o" }),
    });
    renderPage();

    const llmCard = await cardOf("LLM Provider");
    await within(llmCard).findByText("OpenAI · gpt-4o");
    await userEvent.click(within(llmCard).getByRole("button", { name: "Desconectar" }));

    const dialogTitle = await screen.findByRole("heading", { name: /desconectar/i });
    const dialog = dialogTitle.closest('[role="dialog"]') as HTMLElement;
    await userEvent.click(within(dialog).getByRole("button", { name: "Desconectar" }));

    await waitFor(() => expect(screen.queryByRole("heading", { name: /desconectar/i })).not.toBeInTheDocument());
    expect(within(await cardOf("LLM Provider")).getByText("Não configurado")).toBeInTheDocument();
  });

  it("owner cancels the LLM Provider disconnect dialog - card stays connected (INTGCARD-04)", async () => {
    await loginAs("owner@vane.app");
    await apiFetch("/api/integrations/llm/openai", {
      method: "POST",
      body: JSON.stringify({ api_key: "sk-real-key", model: "gpt-4o" }),
    });
    renderPage();

    const llmCard = await cardOf("LLM Provider");
    await within(llmCard).findByText("OpenAI · gpt-4o");
    await userEvent.click(within(llmCard).getByRole("button", { name: "Desconectar" }));

    const dialogTitle = await screen.findByRole("heading", { name: /desconectar/i });
    const dialog = dialogTitle.closest('[role="dialog"]') as HTMLElement;
    await userEvent.click(within(dialog).getByRole("button", { name: /cancelar/i }));

    await waitFor(() => expect(screen.queryByRole("heading", { name: /desconectar/i })).not.toBeInTheDocument());
    expect(within(await cardOf("LLM Provider")).getByText("OpenAI · gpt-4o")).toBeInTheDocument();
  });

  it("owner clicks Conectar on LLM Provider and opens the default drawer", async () => {
    await loginAs("owner@vane.app");
    renderPage();

    const llmCard = await cardOf("LLM Provider");
    await within(llmCard).findByText("Não conectado");
    await userEvent.click(within(llmCard).getByRole("button", { name: "Conectar" }));

    expect(await screen.findByRole("heading", { name: "Conectar LLM Provider" })).toBeInTheDocument();
  });

  it("active email provider shows meta 'Ativo'; connected-but-inactive shows 'Verificado' (INTGCARD-02)", async () => {
    await loginAs("owner@vane.app");
    await apiFetch("/api/integrations/email/resend", {
      method: "POST",
      body: JSON.stringify({ api_key: "re-key", from_email: "a@b.com", from_name: "A" }),
    });
    await apiFetch("/api/integrations/email/sendgrid", {
      method: "POST",
      body: JSON.stringify({ api_key: "sg-key", from_email: "a@b.com", from_name: "A" }),
    });
    await apiFetch("/api/integrations/email/resend/activate", { method: "POST" });
    renderPage();

    const resendCard = await cardOf("Resend");
    expect(await within(resendCard).findByText("Ativo")).toBeInTheDocument();

    const sendgridCard = await cardOf("SendGrid");
    expect(await within(sendgridCard).findByText("Verificado")).toBeInTheDocument();
  });

  it("owner activates a connected-but-inactive Resend by clicking Ativar (INTGCARD-01)", async () => {
    await loginAs("owner@vane.app");
    await apiFetch("/api/integrations/email/resend", {
      method: "POST",
      body: JSON.stringify({ api_key: "re-key", from_email: "a@b.com", from_name: "A" }),
    });
    renderPage();

    const resendCard = await cardOf("Resend");
    await within(resendCard).findByText("Verificado");
    await userEvent.click(within(resendCard).getByRole("button", { name: "Ativar" }));

    await waitFor(async () => expect(await within(resendCard).findByText("Ativo")).toBeInTheDocument());
    expect(within(resendCard).queryByRole("button", { name: "Ativar" })).not.toBeInTheDocument();
  });

  it("owner activates a connected-but-inactive SendGrid by clicking Ativar (INTGCARD-01)", async () => {
    await loginAs("owner@vane.app");
    await apiFetch("/api/integrations/email/sendgrid", {
      method: "POST",
      body: JSON.stringify({ api_key: "sg-key", from_email: "a@b.com", from_name: "A" }),
    });
    renderPage();

    const sendgridCard = await cardOf("SendGrid");
    await within(sendgridCard).findByText("Verificado");
    await userEvent.click(within(sendgridCard).getByRole("button", { name: "Ativar" }));

    await waitFor(async () => expect(await within(sendgridCard).findByText("Ativo")).toBeInTheDocument());
  });

  it("the Desconectar button is hidden when not connected and Ativar is hidden when already active (INTGCARD-03)", async () => {
    await loginAs("owner@vane.app");
    renderPage();

    const resendCard = await cardOf("Resend");
    await within(resendCard).findByText("Não conectado");
    expect(within(resendCard).queryByRole("button", { name: "Desconectar" })).not.toBeInTheDocument();
  });

  it("owner cancels the Resend disconnect dialog - card stays connected (INTGCARD-04)", async () => {
    await loginAs("owner@vane.app");
    await apiFetch("/api/integrations/email/resend", {
      method: "POST",
      body: JSON.stringify({ api_key: "re-key", from_email: "a@b.com", from_name: "A" }),
    });
    renderPage();

    const resendCard = await cardOf("Resend");
    await within(resendCard).findByText("Verificado");
    await userEvent.click(within(resendCard).getByRole("button", { name: "Desconectar" }));

    const dialogTitle = await screen.findByRole("heading", { name: /desconectar/i });
    const dialog = dialogTitle.closest('[role="dialog"]') as HTMLElement;
    await userEvent.click(within(dialog).getByRole("button", { name: /cancelar/i }));

    await waitFor(() => expect(screen.queryByRole("heading", { name: /desconectar/i })).not.toBeInTheDocument());
    expect(within(await cardOf("Resend")).getByText("Verificado")).toBeInTheDocument();
  });

  it("owner confirms the Resend disconnect dialog - card goes back to Not connected (INTGCARD-04)", async () => {
    await loginAs("owner@vane.app");
    await apiFetch("/api/integrations/email/resend", {
      method: "POST",
      body: JSON.stringify({ api_key: "re-key", from_email: "a@b.com", from_name: "A" }),
    });
    renderPage();

    const resendCard = await cardOf("Resend");
    await within(resendCard).findByText("Verificado");
    await userEvent.click(within(resendCard).getByRole("button", { name: "Desconectar" }));

    const dialogTitle = await screen.findByRole("heading", { name: /desconectar/i });
    const dialog = dialogTitle.closest('[role="dialog"]') as HTMLElement;
    await userEvent.click(within(dialog).getByRole("button", { name: "Desconectar" }));

    await waitFor(() => expect(screen.queryByRole("heading", { name: /desconectar/i })).not.toBeInTheDocument());
    expect(within(await cardOf("Resend")).getByText("Não conectado")).toBeInTheDocument();
  });

  it("failing to activate Resend shows an error on the card and keeps the connected-inactive state (INTGCARD-01 AC4)", async () => {
    await loginAs("owner@vane.app");
    await apiFetch("/api/integrations/email/resend", {
      method: "POST",
      body: JSON.stringify({ api_key: "re-key", from_email: "a@b.com", from_name: "A" }),
    });
    server.use(
      http.post("/api/integrations/email/resend/activate", () => HttpResponse.json({ error: "boom" }, { status: 500 })),
    );
    renderPage();

    const resendCard = await cardOf("Resend");
    await within(resendCard).findByText("Verificado");
    await userEvent.click(within(resendCard).getByRole("button", { name: "Ativar" }));

    expect(await within(resendCard).findByRole("alert")).toHaveTextContent("boom");
    expect(within(resendCard).getByText("Verificado")).toBeInTheDocument();
    expect(within(resendCard).getByRole("button", { name: "Ativar" })).toBeInTheDocument();
  });

  it("failing to activate LLM Provider shows an error on the card and keeps the connected-inactive state (INTGCARD-01 AC4)", async () => {
    await loginAs("owner@vane.app");
    await apiFetch("/api/integrations/llm/openai", {
      method: "POST",
      body: JSON.stringify({ api_key: "sk-real-key", model: "gpt-4o" }),
    });
    server.use(
      http.post("/api/integrations/llm/openai/activate", () => HttpResponse.json({ error: "boom" }, { status: 500 })),
    );
    renderPage();

    const llmCard = await cardOf("LLM Provider");
    await within(llmCard).findByText("OpenAI · gpt-4o");
    await userEvent.click(within(llmCard).getByRole("button", { name: "Ativar" }));

    expect(await within(llmCard).findByRole("alert")).toHaveTextContent("boom");
    expect(within(llmCard).getByText("OpenAI · gpt-4o")).toBeInTheDocument();
    expect(within(llmCard).getByRole("button", { name: "Ativar" })).toBeInTheDocument();
  });

  it("failing to disconnect Resend shows an error on the card and keeps the connected state (INTGCARD-04 AC5)", async () => {
    await loginAs("owner@vane.app");
    await apiFetch("/api/integrations/email/resend", {
      method: "POST",
      body: JSON.stringify({ api_key: "re-key", from_email: "a@b.com", from_name: "A" }),
    });
    server.use(
      http.delete("/api/integrations/email/resend", () => HttpResponse.json({ error: "boom" }, { status: 500 })),
    );
    renderPage();

    const resendCard = await cardOf("Resend");
    await within(resendCard).findByText("Verificado");
    await userEvent.click(within(resendCard).getByRole("button", { name: "Desconectar" }));
    const dialogTitle = await screen.findByRole("heading", { name: /desconectar/i });
    const dialog = dialogTitle.closest('[role="dialog"]') as HTMLElement;
    await userEvent.click(within(dialog).getByRole("button", { name: "Desconectar" }));

    await waitFor(() => expect(screen.queryByRole("heading", { name: /desconectar/i })).not.toBeInTheDocument());
    expect(await within(resendCard).findByRole("alert")).toHaveTextContent("boom");
    expect(within(resendCard).getByText("Verificado")).toBeInTheDocument();
  });

  it("failing to disconnect LLM Provider shows an error on the card and keeps the connected state (INTGCARD-04 AC5)", async () => {
    await loginAs("owner@vane.app");
    await apiFetch("/api/integrations/llm/openai", {
      method: "POST",
      body: JSON.stringify({ api_key: "sk-real-key", model: "gpt-4o" }),
    });
    server.use(
      http.delete("/api/integrations/llm/openai", () => HttpResponse.json({ error: "boom" }, { status: 500 })),
    );
    renderPage();

    const llmCard = await cardOf("LLM Provider");
    await within(llmCard).findByText("OpenAI · gpt-4o");
    await userEvent.click(within(llmCard).getByRole("button", { name: "Desconectar" }));
    const dialogTitle = await screen.findByRole("heading", { name: /desconectar/i });
    const dialog = dialogTitle.closest('[role="dialog"]') as HTMLElement;
    await userEvent.click(within(dialog).getByRole("button", { name: "Desconectar" }));

    await waitFor(() => expect(screen.queryByRole("heading", { name: /desconectar/i })).not.toBeInTheDocument());
    expect(await within(llmCard).findByRole("alert")).toHaveTextContent("boom");
    expect(within(llmCard).getByText("OpenAI · gpt-4o")).toBeInTheDocument();
  });

  it("viewer sees neither Ativar nor Desconectar on a connected email card (INTGCARD-01/03)", async () => {
    await apiFetch("/api/auth/login", {
      method: "POST",
      body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
    });
    await apiFetch("/api/integrations/email/resend", {
      method: "POST",
      body: JSON.stringify({ api_key: "re-key", from_email: "a@b.com", from_name: "A" }),
    });
    await apiFetch("/api/auth/logout", { method: "POST" });

    await loginAs("viewer@vane.app");
    renderPage();

    const resendCard = await cardOf("Resend");
    await within(resendCard).findByText("Verificado");
    expect(within(resendCard).queryByRole("button", { name: "Ativar" })).not.toBeInTheDocument();
    expect(within(resendCard).queryByRole("button", { name: "Desconectar" })).not.toBeInTheDocument();
  });

  it("error loading Datadog is isolated - LLM and email stay normal", async () => {
    server.use(http.get("/api/integrations/datadog/status", () => HttpResponse.json({ error: "boom" }, { status: 500 })));
    await loginAs("owner@vane.app");
    renderPage();

    const datadogCard = await cardOf("Datadog");
    expect(await within(datadogCard).findByText("Não foi possível carregar")).toBeInTheDocument();
    expect(within(datadogCard).queryByRole("button")).not.toBeInTheDocument();

    const llmCard = await cardOf("LLM Provider");
    expect(await within(llmCard).findByText("Não conectado")).toBeInTheDocument();
    expect(within(llmCard).getByRole("button", { name: "Conectar" })).toBeInTheDocument();

    const resendCard = await cardOf("Resend");
    expect(await within(resendCard).findByText("Não conectado")).toBeInTheDocument();
  });

  // AD-034, SAASMAIL-11 AC3: a saas tenant never sees the email-provider
  // category at all - the same treatment LoginPage/SignupPage already give
  // the signup link (AD-033).
  it("saas mode - email provider category and drawer never render", async () => {
    setDeploymentMode("saas");
    await loginAs("owner@vane.app");
    renderPage();

    await screen.findByText("Datadog");
    expect(screen.queryByText("Resend")).not.toBeInTheDocument();
    expect(screen.queryByText("SendGrid")).not.toBeInTheDocument();
    expect(screen.queryByText(i18n.t("integrations.categories.email"))).not.toBeInTheDocument();
  });
});
