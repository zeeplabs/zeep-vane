import { describe, it, expect, afterEach } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse, delay } from "msw";
import { server } from "../../test/msw/server";
import "../../lib/i18n";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch } from "../../lib/apiClient";
import type { Domain, Page, StatusPage } from "../../types/api";
import { StatusPagesTable } from "./StatusPagesTable";

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

afterEach(async () => {
  await apiFetch("/api/auth/logout", { method: "POST" });
});

function mockStatusPagesPage(items: StatusPage[], total = items.length, pageSize = 20) {
  server.use(
    http.get("/api/status-pages", (): HttpResponse<Page<StatusPage>> => {
      return HttpResponse.json({ items, total, page: 1, page_size: pageSize });
    })
  );
}

function mockDomainsPage(items: Domain[]) {
  server.use(
    http.get("/api/domains", (): HttpResponse<Page<Domain>> => {
      return HttpResponse.json({ items, total: items.length, page: 1, page_size: 20 });
    })
  );
}

function baseDomain(overrides: Partial<Domain>): Domain {
  return {
    id: "dom-fixture",
    hostname: "acme.health",
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

function renderTable(onSelect: (page: StatusPage) => void = () => {}) {
  return render(
    <TestQueryProvider>
      <StatusPagesTable onSelect={onSelect} />
    </TestQueryProvider>
  );
}

describe("StatusPagesTable", () => {
  // SKEL-04/05: while /api/status-pages is loading, skeleton rows matching
  // the real grid template render (not the old "Carregando…" paragraph as
  // visible content) inside an aria-busy container that still carries the
  // sr-only loading string.
  it("mostra skeletons (não o texto) enquanto /api/status-pages carrega", async () => {
    mockDomainsPage([]);
    server.use(
      http.get("/api/status-pages", async () => {
        await delay("infinite");
        return HttpResponse.json({ items: [], total: 0, page: 1, page_size: 20 });
      }),
    );
    await loginAsOwner();
    renderTable();

    const srText = await screen.findByText("Carregando…");
    expect(srText.className).toContain("sr-only");
    expect(srText.closest('[aria-busy="true"]')).toBeInTheDocument();
    expect(screen.getAllByTestId("skeleton").length).toBeGreaterThan(0);
  });

  // SKEL-06: once the fetch resolves, skeletons are gone and the real
  // table (or its EmptyState) takes over.
  it("remove os skeletons assim que /api/status-pages termina de carregar", async () => {
    mockDomainsPage([]);
    mockStatusPagesPage([basePage({ id: "sp-loaded", name: "Página Carregada" })]);
    await loginAsOwner();
    renderTable();

    await screen.findByText("Página Carregada");
    expect(screen.queryAllByTestId("skeleton")).toHaveLength(0);
  });

  it("renderiza a URL pública quando a página tem domínio anexado (DSP-13)", async () => {
    mockDomainsPage([baseDomain({ id: "dom-1", hostname: "acme.health" })]);
    mockStatusPagesPage([
      basePage({
        id: "sp-with-domain",
        name: "Com domínio",
        domain_id: "dom-1",
        subdomain: "status",
        service_ids: ["svc-1", "svc-2"],
      }),
    ]);
    await loginAsOwner();
    renderTable();

    await screen.findByText("Com domínio");
    expect(screen.getByText("status.acme.health")).toBeInTheDocument();
    expect(screen.getByText("2 serviços")).toBeInTheDocument();
    expect(screen.getByText("Público")).toBeInTheDocument();
  });

  it("renderiza '—' na coluna URL pública quando a página não tem domínio (DSP-17)", async () => {
    mockDomainsPage([]);
    mockStatusPagesPage([basePage({ id: "sp-no-domain", name: "Sem domínio", domain_id: null, subdomain: null })]);
    await loginAsOwner();
    renderTable();

    await screen.findByText("Sem domínio");
    const row = screen.getAllByTestId("status-page-row")[0];
    expect(within(row).getByText("—")).toBeInTheDocument();
  });

  it("mostra o EmptyState em vez de uma tabela vazia quando não há status pages", async () => {
    mockDomainsPage([]);
    mockStatusPagesPage([]);
    await loginAsOwner();
    renderTable();

    expect(await screen.findByText("Nenhuma status page criada")).toBeInTheDocument();
    expect(screen.queryAllByTestId("status-page-row")).toHaveLength(0);
  });

  it("clicar em uma linha chama onSelect com a página correspondente", async () => {
    mockDomainsPage([]);
    const page = basePage({ id: "sp-click", name: "Clicável" });
    mockStatusPagesPage([page]);
    await loginAsOwner();
    let selected: StatusPage | null = null;
    renderTable((p) => {
      selected = p;
    });

    await screen.findByText("Clicável");
    await userEvent.click(screen.getAllByTestId("status-page-row")[0]);
    expect(selected).toEqual(page);
  });
});
