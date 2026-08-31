import { describe, it, expect, afterEach, vi } from "vitest";
import { http, HttpResponse } from "msw";
import { render, screen, act } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import "../../lib/i18n";
import { AuthProvider } from "../../auth/AuthProvider";
import { TestQueryProvider } from "../../test/queryClient";
import { server } from "../../test/msw/server";
import * as apiClient from "../../lib/apiClient";
import type { Page, StatusPage } from "../../types/api";
import { StatusPageDetail } from "./StatusPageDetail";

async function loginAsOwner() {
  await apiClient.apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

async function loginAs(email: string) {
  await apiClient.apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email, password: "demo1234" }),
  });
}

async function createDomainlessPage(name: string): Promise<StatusPage> {
  return apiClient.apiFetch<StatusPage>("/api/status-pages", {
    method: "POST",
    body: JSON.stringify({ name, service_ids: [] }),
  });
}

afterEach(async () => {
  vi.useRealTimers();
  await apiClient.apiFetch("/api/auth/logout", { method: "POST" });
});

function renderDetail(id: string) {
  return render(
    <MemoryRouter initialEntries={[`/status-pages/${id}`]}>
      <TestQueryProvider>
        <AuthProvider>
          <Routes>
            <Route path="/status-pages/:id" element={<StatusPageDetail />} />
          </Routes>
        </AuthProvider>
      </TestQueryProvider>
    </MemoryRouter>
  );
}

describe("StatusPageDetail", () => {
  it("estado published exibe a URL pública, o link de preview e não faz polling adicional", async () => {
    await loginAsOwner();
    const spy = vi.spyOn(apiClient, "apiFetch");
    renderDetail("sp-1");

    expect(await screen.findByText("Publicada")).toBeInTheDocument();
    expect(await screen.findByText(/https:\/\/status\.status\.acme\.com/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Pré-visualizar página pública" })).toHaveAttribute(
      "href",
      "/status/sp-1"
    );

    const callsAfterLoad = spy.mock.calls.filter((c) => c[0] === "/api/status-pages").length;

    vi.useFakeTimers();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000);
    });

    const callsAfterWait = spy.mock.calls.filter((c) => c[0] === "/api/status-pages").length;
    expect(callsAfterWait).toBe(callsAfterLoad);
  });

  it("estado tls_failed exibe o motivo da falha, o link de preview e não faz polling adicional", async () => {
    await loginAsOwner();
    const spy = vi.spyOn(apiClient, "apiFetch");
    renderDetail("sp-3");

    expect(await screen.findByText("Falha")).toBeInTheDocument();
    expect(
      screen.getByText("Falha ao validar propriedade do domínio via DNS-01.")
    ).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Pré-visualizar página pública" })).toBeInTheDocument();

    const callsAfterLoad = spy.mock.calls.filter((c) => c[0] === "/api/status-pages").length;

    vi.useFakeTimers();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000);
    });

    const callsAfterWait = spy.mock.calls.filter((c) => c[0] === "/api/status-pages").length;
    expect(callsAfterWait).toBe(callsAfterLoad);
  });

  it("sem domínio (domain_id null) exibe label distinto e botão pra anexar domínio (SPD-12)", async () => {
    await loginAsOwner();
    const page = await createDomainlessPage("Detail Sem Domínio Test");
    renderDetail(page.id);

    expect(await screen.findByText("Sem domínio configurado")).toBeInTheDocument();
    expect(screen.queryByText("Aguardando validação de DNS/certificado")).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Pré-visualizar página pública" })).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Anexar domínio" }));
    expect(await screen.findByLabelText("Domínio")).toBeInTheDocument();
  });

  it("domínio anexado + draft exibe o label de DNS/certificado pendente, distinto do 'sem domínio' (SPD-13)", async () => {
    await loginAsOwner();
    renderDetail("sp-2");

    expect(await screen.findByText("Aguardando validação de DNS/certificado")).toBeInTheDocument();
    expect(screen.queryByText("Sem domínio configurado")).not.toBeInTheDocument();
    expect(screen.queryByText("Emitindo certificado")).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Pré-visualizar página pública" })).toBeInTheDocument();
  });

  it("owner com página pending_tls vê o painel de verificação de DNS e pode acionar a checagem", async () => {
    await loginAsOwner();
    renderDetail("sp-2");
    await screen.findByText("Aguardando validação de DNS/certificado");

    expect(screen.getByText("Configuração de DNS")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Verificar DNS/certificado" })).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Verificar DNS/certificado" }));

    // O mock de verify-domain publica a página (ver handlers.ts) - o painel
    // some assim que a mutation invalida a query e o state vira
    // "published" (não dá pra garantir observar o resultado intermediário
    // do painel de forma não-racy, já que ele desmonta no mesmo instante).
    expect(await screen.findByText("Publicada")).toBeInTheDocument();
    expect(screen.queryByText("Configuração de DNS")).not.toBeInTheDocument();
  });

  it("resultado de verificação com DNS incorreto/certificado inválido é exibido sem publicar a página", async () => {
    await loginAsOwner();
    server.use(
      http.post("/api/status-pages/:id/verify-domain", () =>
        HttpResponse.json({
          hostname: "status.beta.com",
          resolved_ips: ["198.51.100.20"],
          dns_resolved: true,
          dns_matches_target: false,
          tls_reachable: true,
          tls_cert_valid: false,
          tls_error: "x509: certificate signed by unknown authority",
          state: "pending_tls",
          tls_last_error: null,
          checked_at: new Date().toISOString(),
        })
      )
    );
    renderDetail("sp-2");
    await screen.findByText("Aguardando validação de DNS/certificado");

    await userEvent.click(screen.getByRole("button", { name: "Verificar DNS/certificado" }));

    expect(await screen.findByText(/DNS resolve para 198.51.100.20, diferente do destino esperado/)).toBeInTheDocument();
    expect(
      await screen.findByText(/Conexão HTTPS respondeu, mas o certificado não é válido/)
    ).toBeInTheDocument();
    // Não publicou - painel continua visível pro admin tentar de novo.
    expect(screen.getByText("Configuração de DNS")).toBeInTheDocument();
    expect(screen.queryByText("Publicada")).not.toBeInTheDocument();
  });

  it("viewer não vê o painel de verificação de DNS (endpoints que ele usa são write-role-gated)", async () => {
    await loginAs("viewer@vane.app");
    renderDetail("sp-2");

    expect(await screen.findByText("Aguardando validação de DNS/certificado")).toBeInTheDocument();
    expect(screen.queryByText("Configuração de DNS")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Verificar DNS/certificado" })).not.toBeInTheDocument();
  });

  it("estado 'published' com domain_id/subdomain nulos (formato defendido, nunca produzido pelo fluxo real) não renderiza URL quebrada nem lança (mutante #5)", async () => {
    await loginAsOwner();
    // Mesmo raciocínio de StatusPagesSection.test.tsx: MarkPublished só
    // marca "published" via um JOIN por hostname que exige domain_id
    // não-nulo, então isso nunca ocorre pelo fluxo real - mas o guard
    // `if (!domainId || !subdomain) return null` em publicUrl() existe
    // como defesa. Fixture forçado via override do MSW.
    const impossiblePublished: StatusPage = {
      id: "sp-impossible-published-detail",
      name: "Página Published Sem Domínio (impossível, detail)",
      subdomain: null,
      domain_id: null,
      state: "published",
      tls_last_error: null,
      created_at: new Date().toISOString(),
      service_ids: [],
    };
    server.use(
      http.get("/api/status-pages", () =>
        HttpResponse.json({ items: [impossiblePublished], total: 1, page: 1, page_size: 20 })
      )
    );

    renderDetail(impossiblePublished.id);

    expect(await screen.findByText("Publicada")).toBeInTheDocument();
    expect(document.body.textContent).not.toContain("https://null");
    expect(document.body.textContent).not.toContain("undefined");
    expect(screen.queryByRole("link", { name: /^https:\/\//i })).not.toBeInTheDocument();
  });

  it("exibe os serviços vinculados marcados e os demais desmarcados", async () => {
    await loginAsOwner();
    renderDetail("sp-1");

    expect(await screen.findByText("Serviços vinculados")).toBeInTheDocument();
    // sp-1 fixture: service_ids = ["svc-1", "svc-2"] (API pública, Checkout).
    // Vinculados são checkboxes marcados no grupo "Vinculados"; os demais
    // aparecem desmarcados no grupo "Disponíveis" (SPD-16 redesign).
    expect(screen.getByRole("checkbox", { name: "API pública" })).toBeChecked();
    expect(screen.getByRole("checkbox", { name: "Checkout" })).toBeChecked();
    expect(screen.getByRole("checkbox", { name: "Notificações" })).not.toBeChecked();
    expect(screen.getByRole("checkbox", { name: "Fila de processamento" })).not.toBeChecked();
  });

  it("owner alterna um serviço e salva, persistindo o novo conjunto via PATCH (SPD-15)", async () => {
    await loginAsOwner();
    renderDetail("sp-1");
    await screen.findByText("Serviços vinculados");

    const saveButton = screen.getByRole("button", { name: "Salvar serviços" });
    expect(saveButton).toBeDisabled();

    await userEvent.click(screen.getByRole("checkbox", { name: "Notificações" }));
    expect(saveButton).toBeEnabled();

    await userEvent.click(saveButton);

    await vi.waitFor(() => expect(saveButton).toBeDisabled());
    expect(screen.getByRole("checkbox", { name: "API pública" })).toBeChecked();
    expect(screen.getByRole("checkbox", { name: "Checkout" })).toBeChecked();
    expect(screen.getByRole("checkbox", { name: "Notificações" })).toBeChecked();

    // Confirma que persistiu de fato (reflete o servidor, não só estado local
    // otimista): reconsulta via GET /api/status-pages.
    const reloaded = await apiClient.apiFetch<Page<StatusPage>>("/api/status-pages?page=1");
    const page = reloaded.items.find((p) => p.id === "sp-1");
    expect(page?.service_ids.sort()).toEqual(["svc-1", "svc-2", "svc-3"]);
  });

  it("desmarcar todos e salvar substitui o conjunto inteiro por vazio (replace-all, não incremental)", async () => {
    await loginAsOwner();
    renderDetail("sp-1");
    await screen.findByText("Serviços vinculados");

    await userEvent.click(screen.getByRole("checkbox", { name: "API pública" }));
    await userEvent.click(screen.getByRole("checkbox", { name: "Checkout" }));
    await userEvent.click(screen.getByRole("button", { name: "Salvar serviços" }));

    await vi.waitFor(async () => {
      const reloaded = await apiClient.apiFetch<Page<StatusPage>>("/api/status-pages?page=1");
      expect(reloaded.items.find((p) => p.id === "sp-1")?.service_ids).toEqual([]);
    });
  });

  it("busca filtra o grupo Disponíveis sem afetar o grupo Vinculados (SPD-16)", async () => {
    await loginAsOwner();
    renderDetail("sp-1");
    await screen.findByText("Serviços vinculados");

    // sp-1 fixture: vinculados = API pública, Checkout; disponíveis =
    // Notificações, Fila de processamento.
    await userEvent.type(screen.getByLabelText("Buscar serviço"), "notif");

    expect(screen.getByRole("checkbox", { name: "Notificações" })).toBeInTheDocument();
    expect(screen.queryByRole("checkbox", { name: "Fila de processamento" })).not.toBeInTheDocument();
    // Vinculados nunca é afetado pelo filtro, mesmo sem match no texto buscado.
    expect(screen.getByRole("checkbox", { name: "API pública" })).toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: "Checkout" })).toBeInTheDocument();
  });

  it("viewer vê os serviços vinculados mas não pode alterá-los nem vê o botão salvar", async () => {
    await loginAs("viewer@vane.app");
    renderDetail("sp-1");

    expect(await screen.findByText("Serviços vinculados")).toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: "API pública" })).toBeDisabled();
    expect(screen.queryByRole("button", { name: "Salvar serviços" })).not.toBeInTheDocument();
  });
});
