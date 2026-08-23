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
import type { StatusPage } from "../../types/api";
import { StatusPageDetail } from "./StatusPageDetail";

async function loginAsOwner() {
  await apiClient.apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
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
      http.get("/api/status-pages", () => HttpResponse.json([impossiblePublished]))
    );

    renderDetail(impossiblePublished.id);

    expect(await screen.findByText("Publicada")).toBeInTheDocument();
    expect(document.body.textContent).not.toContain("https://null");
    expect(document.body.textContent).not.toContain("undefined");
    expect(screen.queryByRole("link", { name: /^https:\/\//i })).not.toBeInTheDocument();
  });
});
