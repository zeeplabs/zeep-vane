import { describe, it, expect, afterEach } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { http, HttpResponse, delay } from "msw";
import "../../lib/i18n";
import { server } from "../../test/msw/server";
import { resetDeploymentMode, setDeploymentMode } from "../../test/msw/handlers";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch } from "../../lib/apiClient";
import { AuthProvider } from "../../auth/AuthProvider";
import { SettingsPage } from "./SettingsPage";
import type { CompanySettings } from "../../types/api";

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

afterEach(async () => {
  await apiFetch("/api/auth/logout", { method: "POST" });
  resetDeploymentMode();
});

function renderPage() {
  return render(
    <MemoryRouter>
      <TestQueryProvider>
        <AuthProvider>
          <SettingsPage />
        </AuthProvider>
      </TestQueryProvider>
    </MemoryRouter>,
  );
}

describe("SettingsPage", () => {
  // SKEL-04/05: while /api/company-settings is loading, skeleton blocks
  // render (not the old "Carregando…" paragraph as visible content)
  // inside an aria-busy container that still carries the sr-only string.
  it("mostra skeletons (não o texto) enquanto /api/company-settings carrega", async () => {
    server.use(
      http.get("/api/company-settings", async () => {
        await delay("infinite");
        return HttpResponse.json({});
      }),
    );
    await loginAsOwner();
    renderPage();

    const srText = await screen.findByText("Carregando…");
    expect(srText.className).toContain("sr-only");
    expect(srText.closest('[aria-busy="true"]')).toBeInTheDocument();
    expect(screen.getAllByTestId("skeleton").length).toBeGreaterThan(0);
  });

  // SKEL-06: once the fetch resolves, skeletons are gone and the real form
  // takes over.
  it("remove os skeletons assim que /api/company-settings termina de carregar", async () => {
    await loginAsOwner();
    renderPage();

    await screen.findByDisplayValue("Sua Empresa Ltda.");
    expect(screen.queryAllByTestId("skeleton")).toHaveLength(0);
  });

  it("carrega e exibe o perfil da empresa persistido (nome/site/fuso/idioma)", async () => {
    await loginAsOwner();
    renderPage();

    expect(await screen.findByDisplayValue("Sua Empresa Ltda.")).toBeInTheDocument();
    expect(screen.getByLabelText("Site")).toHaveValue("");
    expect(screen.getByLabelText("Fuso horário")).toHaveValue("America/Sao_Paulo (GMT-3)");
    expect(screen.getByLabelText("Idioma")).toHaveValue("pt-BR");
  });

  it("editar nome/site/fuso e salvar persiste os 3 campos (CFGPG-01/02)", async () => {
    await loginAsOwner();
    renderPage();
    await screen.findByDisplayValue("Sua Empresa Ltda.");

    const nameInput = screen.getByLabelText("Nome da empresa");
    await userEvent.clear(nameInput);
    await userEvent.type(nameInput, "Empresa Editada");
    await userEvent.type(screen.getByLabelText("Site"), "https://empresa.example.com");
    await userEvent.selectOptions(screen.getByLabelText("Fuso horário"), "UTC (GMT+0)");

    await userEvent.click(screen.getByRole("button", { name: "Salvar alterações" }));

    expect(await screen.findByText("Alterações salvas")).toBeInTheDocument();

    const persisted = await apiFetch<CompanySettings>("/api/company-settings");
    expect(persisted.name).toBe("Empresa Editada");
    expect(persisted.website).toBe("https://empresa.example.com");
    expect(persisted.timezone).toBe("UTC (GMT+0)");
  });

  it("fuso horário inválido do backend mostra o erro inline (CFGPG-04)", async () => {
    server.use(
      http.patch("/api/company-settings", () =>
        HttpResponse.json({ error: "timezone must be one of the supported values" }, { status: 422 }),
      ),
    );
    await loginAsOwner();
    renderPage();
    await screen.findByDisplayValue("Sua Empresa Ltda.");

    await userEvent.click(screen.getByRole("button", { name: "Salvar alterações" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("timezone must be one of the supported values");
  });

  it("selecionar um arquivo de logo dispara o upload imediatamente", async () => {
    await loginAsOwner();
    renderPage();
    await screen.findByDisplayValue("Sua Empresa Ltda.");

    const file = new File(["fake-png-bytes"], "logo.png", { type: "image/png" });
    const fileInput = document.querySelector('input[type="file"]') as HTMLInputElement;
    await userEvent.upload(fileInput, file);

    await waitFor(async () => {
      const updated = await apiFetch<CompanySettings>("/api/company-settings");
      expect(updated.logo_url).toBe("/uploads/logo");
    });
  });

  it("falha de upload (422) exibe o erro inline", async () => {
    server.use(
      http.post("/api/company-settings/logo", () =>
        HttpResponse.json({ error: "logo must be a PNG or SVG image no larger than 10 MB" }, { status: 422 }),
      ),
    );
    await loginAsOwner();
    renderPage();
    await screen.findByDisplayValue("Sua Empresa Ltda.");

    const file = new File(["fake-png-bytes"], "logo.png", { type: "image/png" });
    const fileInput = document.querySelector('input[type="file"]') as HTMLInputElement;
    await userEvent.upload(fileInput, file);

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "logo must be a PNG or SVG image no larger than 10 MB",
    );
  });

  it("tipo de pessoa Pessoa Jurídica mostra Razão social/CNPJ e inscrição estadual (CFGPG-06)", async () => {
    await loginAsOwner();
    renderPage();
    await screen.findByDisplayValue("Sua Empresa Ltda.");

    expect(screen.getByRole("radio", { name: "Pessoa Jurídica" })).toHaveAttribute("aria-checked", "true");
    expect(screen.getByLabelText("Razão social")).toBeInTheDocument();
    expect(screen.getByLabelText("CNPJ")).toBeInTheDocument();
    expect(screen.getByLabelText("Inscrição estadual")).toBeInTheDocument();
  });

  it("alternar para Pessoa Física troca os rótulos e esconde inscrição estadual (CFGPG-06)", async () => {
    await loginAsOwner();
    renderPage();
    await screen.findByDisplayValue("Sua Empresa Ltda.");

    await userEvent.click(screen.getByRole("radio", { name: "Pessoa Física" }));

    expect(screen.getByLabelText("Nome completo")).toBeInTheDocument();
    expect(screen.getByLabelText("CPF")).toBeInTheDocument();
    expect(screen.queryByLabelText("Inscrição estadual")).not.toBeInTheDocument();
  });

  it("preencher razão social, CNPJ e endereço fiscal completo persiste tudo (CFGPG-06/07)", async () => {
    await loginAsOwner();
    renderPage();
    await screen.findByDisplayValue("Sua Empresa Ltda.");

    await userEvent.type(screen.getByLabelText("Razão social"), "Acme Comercio Ltda");
    await userEvent.type(screen.getByLabelText("CNPJ"), "12345678000199");
    await userEvent.type(screen.getByLabelText("CEP"), "01310-100");
    await userEvent.type(screen.getByLabelText("Endereço"), "Av. Paulista");
    await userEvent.type(screen.getByLabelText("Número"), "1000");
    await userEvent.type(screen.getByLabelText("Cidade"), "São Paulo");
    await userEvent.type(screen.getByLabelText("País"), "Brasil");

    await userEvent.click(screen.getByRole("button", { name: "Salvar alterações" }));
    await screen.findByText("Alterações salvas");

    const persisted = await apiFetch<CompanySettings>("/api/company-settings");
    expect(persisted.legal_name).toBe("Acme Comercio Ltda");
    expect(persisted.tax_id).toBe("12345678000199");
    expect(persisted.tax_id_type).toBe("cnpj");
    expect(persisted.billing_address).toEqual({
      zip: "01310-100",
      street: "Av. Paulista",
      number: "1000",
      complement: "",
      state: "",
      city: "São Paulo",
      country: "Brasil",
    });
  });

  it("CPF com menos de 11 dígitos mostra o erro de validação do backend, sem persistir (TENANT-23)", async () => {
    await loginAsOwner();
    renderPage();
    await screen.findByDisplayValue("Sua Empresa Ltda.");

    await userEvent.click(screen.getByRole("radio", { name: "Pessoa Física" }));
    await userEvent.type(screen.getByLabelText("CPF"), "1234");
    await userEvent.click(screen.getByRole("button", { name: "Salvar alterações" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "tax_id must have 11 digits for cpf or 14 digits for cnpj",
    );

    const persisted = await apiFetch<CompanySettings>("/api/company-settings");
    expect(persisted.tax_id ?? "").not.toBe("1234");
  });

  it("descartar reverte os campos ao último valor persistido, sem chamar a API", async () => {
    await loginAsOwner();
    renderPage();
    await screen.findByDisplayValue("Sua Empresa Ltda.");

    const nameInput = screen.getByLabelText("Nome da empresa");
    await userEvent.clear(nameInput);
    await userEvent.type(nameInput, "Rascunho não salvo");

    await userEvent.click(screen.getByRole("button", { name: "Descartar" }));

    expect(screen.getByLabelText("Nome da empresa")).toHaveValue("Sua Empresa Ltda.");
    const persisted = await apiFetch<CompanySettings>("/api/company-settings");
    expect(persisted.name).toBe("Sua Empresa Ltda.");
  });

  it("botão Excluir conta abre modal de confirmação antes de qualquer chamada de API (CFGPG-09)", async () => {
    await loginAsOwner();
    renderPage();
    await screen.findByDisplayValue("Sua Empresa Ltda.");

    await userEvent.click(screen.getByRole("button", { name: "Excluir conta" }));

    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText("Excluir conta?")).toBeInTheDocument();
  });

  it("confirmar exclusão chama DELETE /api/tenants/current e fecha o modal (CFGPG-09)", async () => {
    await loginAsOwner();
    renderPage();
    await screen.findByDisplayValue("Sua Empresa Ltda.");

    await userEvent.click(screen.getByRole("button", { name: "Excluir conta" }));
    const dialog = await screen.findByRole("dialog");
    await userEvent.click(within(dialog).getByRole("button", { name: "Excluir conta" }));

    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });

  it("exclusão bloqueada (409, único tenant ativo) mostra o erro dentro do modal, que permanece aberto (CFGPG-11)", async () => {
    server.use(
      http.delete("/api/tenants/current", () =>
        HttpResponse.json({ error: "this is your only active account - it cannot be deleted" }, { status: 409 }),
      ),
    );
    await loginAsOwner();
    renderPage();
    await screen.findByDisplayValue("Sua Empresa Ltda.");

    await userEvent.click(screen.getByRole("button", { name: "Excluir conta" }));
    const dialog = await screen.findByRole("dialog");
    await userEvent.click(within(dialog).getByRole("button", { name: "Excluir conta" }));

    expect(await within(dialog).findByRole("alert")).toHaveTextContent(
      "this is your only active account - it cannot be deleted",
    );
    expect(screen.getByRole("dialog")).toBeInTheDocument();
  });

  it("self_hosted: zona de perigo (Excluir conta) não é renderizada - AD-002, single-tenant por instalação", async () => {
    setDeploymentMode("self_hosted");
    await loginAsOwner();
    renderPage();
    await screen.findByDisplayValue("Sua Empresa Ltda.");

    expect(screen.queryByRole("button", { name: "Excluir conta" })).not.toBeInTheDocument();
    expect(screen.queryByText(/Remove permanentemente esta conta/)).not.toBeInTheDocument();
  });
});
