import { describe, it, expect, afterEach } from "vitest";
import i18n from "../../lib/i18n";
import { render, screen, fireEvent } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { http, HttpResponse } from "msw";
import { server } from "../../test/msw/server";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch } from "../../lib/apiClient";
import { AuthProvider } from "../../auth/AuthProvider";
import type { Domain, Page, StatusPage } from "../../types/api";
import { DomainsStatusPagesPage } from "./DomainsStatusPagesPage";

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

afterEach(async () => {
  await apiFetch("/api/auth/logout", { method: "POST" });
});

function mockDomainsPage(items: Domain[]) {
  server.use(
    http.get("/api/domains", (): HttpResponse<Page<Domain>> => {
      return HttpResponse.json({ items, total: items.length, page: 1, page_size: 20 });
    })
  );
}

function mockStatusPagesPage(items: StatusPage[]) {
  server.use(
    http.get("/api/status-pages", (): HttpResponse<Page<StatusPage>> => {
      return HttpResponse.json({ items, total: items.length, page: 1, page_size: 20 });
    })
  );
}

function baseDomain(overrides: Partial<Domain>): Domain {
  return {
    id: "dom-fixture",
    hostname: "status.example.com",
    created_at: new Date().toISOString(),
    domain_type: "custom",
    status: "verified",
    ssl_status: "active",
    verified_at: new Date().toISOString(),
    last_error: null,
    attached_page_name: null,
    attached_page_count: 0,
    ...overrides,
  };
}

function basePage(overrides: Partial<StatusPage>): StatusPage {
  return {
    id: "sp-fixture",
    name: "Status Página",
    subdomain: null,
    domain_id: null,
    state: "draft",
    tls_last_error: null,
    created_at: new Date().toISOString(),
    service_ids: [],
    ...overrides,
  };
}

function renderPage() {
  return render(
    <TestQueryProvider>
      <MemoryRouter>
        <AuthProvider>
          <DomainsStatusPagesPage />
        </AuthProvider>
      </MemoryRouter>
    </TestQueryProvider>
  );
}

describe("DomainsStatusPagesPage", () => {
  it("renderiza sem crashar, com a aba Domínios ativa por padrão e o botão 'Adicionar domínio'", async () => {
    mockDomainsPage([baseDomain({ id: "dom-1", hostname: "one.example.com" })]);
    mockStatusPagesPage([]);
    await loginAsOwner();
    renderPage();

    await screen.findByText("one.example.com");
    expect(screen.getByRole("tab", { name: "Domínios" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("button", { name: /Adicionar domínio/ })).toBeInTheDocument();
  });

  it("trocar para a aba Status Pages troca a tabela e o rótulo do botão para 'Criar status page' (DSP-18)", async () => {
    mockDomainsPage([]);
    mockStatusPagesPage([basePage({ id: "sp-1", name: "Página Um" })]);
    await loginAsOwner();
    renderPage();

    await userEvent.click(screen.getByRole("tab", { name: "Status Pages" }));

    await screen.findByText("Página Um");
    expect(screen.getByRole("tab", { name: "Status Pages" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("button", { name: /Criar status page/ })).toBeInTheDocument();
  });

  it("trocar de aba fecha o drawer de detalhe aberto na aba anterior (DSP-19)", async () => {
    mockDomainsPage([baseDomain({ id: "dom-1", hostname: "click.example.com" })]);
    mockStatusPagesPage([]);
    await loginAsOwner();
    renderPage();

    await userEvent.click(await screen.findByText("click.example.com"));
    expect(await screen.findByRole("button", { name: "Verificar novamente" })).toBeInTheDocument();

    // Radix's open Dialog marks the rest of the page aria-hidden (and its
    // full-viewport overlay would intercept a real pointer click on the tab
    // underneath) - fireEvent bypasses both to exercise switchTab's own
    // reset directly, since that's what DSP-19 is actually about.
    fireEvent.click(screen.getByText("Status Pages"));

    expect(screen.queryByRole("button", { name: "Verificar novamente" })).not.toBeInTheDocument();

    // DSP-19 requires the *state* to reset, not just the drawer being
    // hidden by which tab is conditionally rendered - switching back to
    // Domínios (where the row is rendered again) must not resurrect the
    // drawer. A mutant that only guards the drawer's render on
    // `activeTab === "domains"` (without actually clearing selectedDomainId)
    // would still pass the assertion above but fail this one.
    fireEvent.click(screen.getByText("Domínios"));
    await screen.findByText("click.example.com");
    expect(screen.queryByRole("button", { name: "Verificar novamente" })).not.toBeInTheDocument();
  });

  it("renderiza em inglês quando o idioma ativo é en", async () => {
    mockDomainsPage([baseDomain({ id: "dom-1", hostname: "one.example.com" })]);
    mockStatusPagesPage([]);
    await loginAsOwner();
    await i18n.changeLanguage("en");

    try {
      renderPage();

      expect(await screen.findByText("Domains & Status Pages")).toBeInTheDocument();
      expect(screen.getByRole("tab", { name: "Domains" })).toBeInTheDocument();
      expect(screen.getByRole("button", { name: /Add domain/ })).toBeInTheDocument();
    } finally {
      await i18n.changeLanguage("pt-BR");
    }
  });
});
