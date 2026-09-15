import { describe, it, expect, afterEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { http, HttpResponse } from "msw";
import { server } from "../../test/msw/server";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch } from "../../lib/apiClient";
import type { Domain, Page, StatusPage } from "../../types/api";
import { StatusPageDetailDrawer } from "./StatusPageDetailDrawer";

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

function renderDrawer(page: StatusPage | null, onClose: () => void = () => {}) {
  return render(
    <MemoryRouter>
      <TestQueryProvider>
        <StatusPageDetailDrawer page={page} onClose={onClose} />
      </TestQueryProvider>
    </MemoryRouter>
  );
}

describe("StatusPageDetailDrawer", () => {
  it("com domínio anexado: mostra o domínio e um link 'Ver página pública' com o href correto (DSP-14)", async () => {
    mockDomainsPage([baseDomain({ id: "dom-1", hostname: "acme.health" })]);
    await loginAsOwner();
    const page = basePage({ id: "sp-1", name: "Com domínio", domain_id: "dom-1", subdomain: "status" });
    renderDrawer(page);

    expect(await screen.findByText("acme.health")).toBeInTheDocument();
    const publicLink = screen.getByRole("link", { name: "Ver página pública" });
    expect(publicLink).toHaveAttribute("href", "https://status.acme.health");

    const editLink = screen.getByRole("link", { name: "Editar página" });
    expect(editLink).toHaveAttribute("href", "/status-pages/sp-1");
  });

  it("sem domínio: não mostra o link 'Ver página pública' e exibe '—' para o domínio (DSP-17)", async () => {
    mockDomainsPage([]);
    await loginAsOwner();
    const page = basePage({ id: "sp-2", name: "Sem domínio", domain_id: null, subdomain: null });
    renderDrawer(page);

    await screen.findByText("Sem domínio");
    expect(screen.queryByRole("link", { name: "Ver página pública" })).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Editar página" })).toHaveAttribute("href", "/status-pages/sp-2");
    // Both the Serviços (empty list) and Domínio (no domain_id) fields
    // render "—" for this fixture - assert both dashes rather than a single
    // ambiguous match.
    expect(screen.getAllByText("—")).toHaveLength(2);
  });
});
