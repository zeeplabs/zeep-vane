import { describe, it, expect, afterEach } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import i18n from "../../lib/i18n";
import { AuthProvider } from "../../auth/AuthProvider";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch } from "../../lib/apiClient";
import { AISettings } from "./AISettings";

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
          <AISettings />
        </AuthProvider>
      </TestQueryProvider>
    </MemoryRouter>
  );
}

describe("AISettings", () => {
  it("mostra OpenAI como não conectado quando nada foi configurado (empty state)", async () => {
    await loginAs("owner@vane.app");
    renderPage();

    await screen.findByText("OpenAI");
    expect(screen.getAllByText("Nenhuma integração conectada").length).toBeGreaterThan(0);
  });

  it("viewer não vê botões de conectar/ativar (read-only)", async () => {
    await loginAs("viewer@vane.app");
    renderPage();

    await screen.findByText("OpenAI");
    expect(screen.queryByRole("button", { name: "Conectar" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Ativar" })).not.toBeInTheDocument();
  });

  it("conectar mostra erro inline em 422 e depois conecta com sucesso", async () => {
    await loginAs("owner@vane.app");
    renderPage();

    await userEvent.click(await screen.findByRole("button", { name: "Conectar" }));
    await userEvent.type(screen.getByLabelText("OpenAI API key"), "invalid-key");
    await userEvent.click(screen.getByRole("button", { name: "Salvar" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(/invalid llm provider api key/);

    // O form permanece aberto após o erro - corrige a chave e salva de novo.
    await userEvent.clear(screen.getByLabelText("OpenAI API key"));
    await userEvent.type(screen.getByLabelText("OpenAI API key"), "sk-real-key");
    await userEvent.click(screen.getByRole("button", { name: "Salvar" }));

    await waitFor(() => expect(screen.getByText("Conectado")).toBeInTheDocument());
    expect(screen.queryByLabelText("OpenAI API key")).not.toBeInTheDocument();
  });

  it("trocar o modelo de um provider já conectado não reabre o formulário de chave", async () => {
    await loginAs("owner@vane.app");
    // Seeds a connected provider through the real connect flow (not a
    // server.use() override), so the in-memory fixture state stays
    // consistent for the model-change call the test drives next.
    await apiFetch("/api/integrations/llm/openai", {
      method: "POST",
      body: JSON.stringify({ api_key: "sk-real-key" }),
    });
    renderPage();

    const select = await screen.findByLabelText("Modelo");
    expect((select as HTMLSelectElement).value).toBe("gpt-4o-mini");

    await userEvent.selectOptions(select, "gpt-4o");

    await waitFor(() => expect((select as HTMLSelectElement).value).toBe("gpt-4o"));
    expect(screen.queryByLabelText("OpenAI API key")).not.toBeInTheDocument();
  });

  it("ativar um provider conectado atualiza o status exibido", async () => {
    await loginAs("owner@vane.app");
    await apiFetch("/api/integrations/llm/openai", {
      method: "POST",
      body: JSON.stringify({ api_key: "sk-real-key" }),
    });
    renderPage();

    expect(await screen.findByText("Conectado")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Ativar" }));

    await waitFor(() => expect(screen.getByText("Ativo")).toBeInTheDocument());
    expect(screen.queryByRole("button", { name: "Ativar" })).not.toBeInTheDocument();
  });

  it("todas as chaves aiSettings.* usadas por AISettings.tsx existem em pt e en", () => {
    // Cobertura estrutural (AGENTS.md §5): garante que nenhuma chave usada
    // por useTranslation() em AISettings.tsx fica sem entrada em um dos
    // dois idiomas, o que renderizaria a chave literal em produção.
    const keys = [
      "title",
      "subtitle",
      "providerLabel",
      "notConnected",
      "connected",
      "active",
      "invalid",
      "connectButton",
      "reconnectButton",
      "activateButton",
      "cancelButton",
      "saveButton",
      "apiKeyLabel",
      "modelLabel",
      "keyHint",
      "lastChecked",
      "genericConnectError",
      "genericActivateError",
      "genericModelError",
    ];
    for (const lng of ["pt", "en"]) {
      for (const key of keys) {
        expect(i18n.getResource(lng, "translation", `aiSettings.${key}`)).toBeTruthy();
      }
    }
  });
});
