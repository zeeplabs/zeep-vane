import { describe, it, expect, afterEach } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { server } from "../../test/msw/server";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch } from "../../lib/apiClient";
import type { Domain, Page } from "../../types/api";
import { DomainsTable } from "./DomainsTable";

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

afterEach(async () => {
  await apiFetch("/api/auth/logout", { method: "POST" });
});

function mockDomainsPage(items: Domain[], total = items.length, pageSize = 20) {
  server.use(
    http.get("/api/domains", (): HttpResponse<Page<Domain>> => {
      return HttpResponse.json({ items, total, page: 1, page_size: pageSize });
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

function renderTable(onSelect: (domain: Domain) => void = () => {}) {
  return render(
    <TestQueryProvider>
      <DomainsTable onSelect={onSelect} />
    </TestQueryProvider>
  );
}

describe("DomainsTable", () => {
  it("renderiza '—' na coluna Aponta para quando o domínio não tem status page anexada (DSP-02)", async () => {
    mockDomainsPage([baseDomain({ id: "dom-0", hostname: "zero.example.com", attached_page_name: null, attached_page_count: 0 })]);
    await loginAsOwner();
    renderTable();

    await screen.findByText("zero.example.com");
    const row = screen.getAllByTestId("domain-row")[0];
    expect(within(row).getByText("—")).toBeInTheDocument();
  });

  it("renderiza o nome da página anexada quando há exatamente uma (DSP-03)", async () => {
    mockDomainsPage([
      baseDomain({ id: "dom-1", hostname: "one.example.com", attached_page_name: "Status Principal", attached_page_count: 1 }),
    ]);
    await loginAsOwner();
    renderTable();

    await screen.findByText("one.example.com");
    expect(screen.getByText("Status Principal")).toBeInTheDocument();
  });

  it("renderiza o nome da primeira página + '+N' quando há mais de uma anexada (DSP-04)", async () => {
    mockDomainsPage([
      baseDomain({ id: "dom-2", hostname: "two.example.com", attached_page_name: "Status Público", attached_page_count: 2 }),
    ]);
    await loginAsOwner();
    renderTable();

    await screen.findByText("two.example.com");
    expect(screen.getByText("Status Público +1")).toBeInTheDocument();
  });

  it("renderiza '—' na coluna Verificado quando verified_at é null (DSP-16)", async () => {
    mockDomainsPage([
      baseDomain({
        id: "dom-null",
        hostname: "null.example.com",
        status: "pending",
        verified_at: null,
        attached_page_name: null,
        attached_page_count: 0,
      }),
    ]);
    await loginAsOwner();
    renderTable();

    await screen.findByText("null.example.com");
    const row = screen.getAllByTestId("domain-row")[0];
    // Both the Aponta para (DSP-02, zero attached) and Verificado (DSP-16,
    // null verified_at) columns render "—" for this fixture - assert both
    // dashes are present rather than a single ambiguous getByText match.
    expect(within(row).getAllByText("—")).toHaveLength(2);
  });

  it("mostra o EmptyState em vez de uma tabela vazia quando não há domínios", async () => {
    mockDomainsPage([]);
    await loginAsOwner();
    renderTable();

    expect(await screen.findByText("Nenhum domínio cadastrado")).toBeInTheDocument();
    expect(screen.queryAllByTestId("domain-row")).toHaveLength(0);
  });

  it("clicar em uma linha chama onSelect com o domínio correspondente", async () => {
    const domain = baseDomain({ id: "dom-click", hostname: "click.example.com" });
    mockDomainsPage([domain]);
    await loginAsOwner();
    let selected: Domain | null = null;
    renderTable((d) => {
      selected = d;
    });

    await screen.findByText("click.example.com");
    await userEvent.click(screen.getAllByTestId("domain-row")[0]);
    expect(selected).toEqual(domain);
  });
});
