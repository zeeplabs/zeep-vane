import { describe, it, expect, afterEach } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse, delay } from "msw";
import "../../lib/i18n";
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
    verified_at: new Date().toISOString(),
    last_error: null,
    verification_txt_value: "dom-fixture-token",
    attached_page_name: null,
    attached_page_count: 0,
    expires_at: null,
    registrar: null,
    expected_ns: null,
    current_ns: null,
    ns_drift_detected: false,
    last_rdap_check_at: null,
    rdap_last_error: null,
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
  // SKEL-04/05: while /api/domains is loading, skeleton rows render (not
  // the old "Carregando…" paragraph as visible content) inside an
  // aria-busy container that still carries the sr-only loading string.
  it("shows skeletons (not the text) while /api/domains loads", async () => {
    server.use(
      http.get("/api/domains", async () => {
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
  // table (or its empty state) takes over.
  it("removes the skeletons as soon as /api/domains finishes loading", async () => {
    mockDomainsPage([baseDomain({ id: "dom-loaded", hostname: "loaded.example.com" })]);
    await loginAsOwner();
    renderTable();

    await screen.findByText("loaded.example.com");
    expect(screen.queryAllByTestId("skeleton")).toHaveLength(0);
  });

  it("renders the row's Status/Tipo and no SSL column (DSP-01, DATV-07)", async () => {
    mockDomainsPage([
      baseDomain({
        id: "dom-full",
        hostname: "full.example.com",
        domain_type: "custom",
        status: "verified",
      }),
    ]);
    await loginAsOwner();
    renderTable();

    await screen.findByText("full.example.com");
    const row = screen.getAllByTestId("domain-row")[0];
    expect(within(row).getByText("Verificado")).toBeInTheDocument();
    expect(within(row).getByText("Domínio próprio")).toBeInTheDocument();
    // DATV-07: the SSL column (header and per-row status cell) is gone.
    expect(screen.queryByText("SSL")).not.toBeInTheDocument();
    expect(within(row).queryByText("Ativo")).not.toBeInTheDocument();
  });

  it("renders '—' in the Aponta para column when the domain has no attached status page (DSP-02)", async () => {
    mockDomainsPage([baseDomain({ id: "dom-0", hostname: "zero.example.com", attached_page_name: null, attached_page_count: 0 })]);
    await loginAsOwner();
    renderTable();

    await screen.findByText("zero.example.com");
    const row = screen.getAllByTestId("domain-row")[0];
    expect(within(row).getByText("—")).toBeInTheDocument();
  });

  it("renders the attached page's name when there is exactly one (DSP-03)", async () => {
    mockDomainsPage([
      baseDomain({ id: "dom-1", hostname: "one.example.com", attached_page_name: "Status Principal", attached_page_count: 1 }),
    ]);
    await loginAsOwner();
    renderTable();

    await screen.findByText("one.example.com");
    expect(screen.getByText("Status Principal")).toBeInTheDocument();
  });

  it("renders the first page's name + '+N' when more than one is attached (DSP-04)", async () => {
    mockDomainsPage([
      baseDomain({ id: "dom-2", hostname: "two.example.com", attached_page_name: "Status Público", attached_page_count: 2 }),
    ]);
    await loginAsOwner();
    renderTable();

    await screen.findByText("two.example.com");
    expect(screen.getByText("Status Público +1")).toBeInTheDocument();
  });

  it("renders '—' in the Verificado column when verified_at is null (DSP-16)", async () => {
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

  it("shows the EmptyState instead of an empty table when there are no domains", async () => {
    mockDomainsPage([]);
    await loginAsOwner();
    renderTable();

    expect(await screen.findByText("Nenhum domínio cadastrado")).toBeInTheDocument();
    expect(screen.queryAllByTestId("domain-row")).toHaveLength(0);
  });

  it("clicking a row calls onSelect with the corresponding domain", async () => {
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

  it("shows the drift indicator for a domain with ns_drift_detected (DHM-08)", async () => {
    mockDomainsPage([
      baseDomain({
        id: "dom-drift",
        hostname: "drift-row.example.com",
        ns_drift_detected: true,
        expires_at: new Date(Date.now() + 200 * 24 * 60 * 60 * 1000).toISOString(),
      }),
    ]);
    await loginAsOwner();
    renderTable();

    await screen.findByText("drift-row.example.com");
    const row = screen.getAllByTestId("domain-row")[0];
    const badge = within(row).getByTestId("domain-at-risk");
    expect(badge).toHaveAttribute("data-risk", "drift");
    expect(badge).toHaveTextContent("Drift de NS");
  });

  it("shows the expiring-soon indicator for a domain expiring within 30 days (DHM-08)", async () => {
    mockDomainsPage([
      baseDomain({
        id: "dom-expiring",
        hostname: "expiring-row.example.com",
        ns_drift_detected: false,
        expires_at: new Date(Date.now() + 10 * 24 * 60 * 60 * 1000).toISOString(),
      }),
    ]);
    await loginAsOwner();
    renderTable();

    await screen.findByText("expiring-row.example.com");
    const row = screen.getAllByTestId("domain-row")[0];
    const badge = within(row).getByTestId("domain-at-risk");
    expect(badge).toHaveAttribute("data-risk", "expiring");
    expect(badge).toHaveTextContent("Expira em breve");
  });

  it("shows the expired indicator, not expiring-soon, for a domain past its expiration date", async () => {
    mockDomainsPage([
      baseDomain({
        id: "dom-expired",
        hostname: "expired-row.example.com",
        ns_drift_detected: false,
        expires_at: new Date(Date.now() - 3 * 24 * 60 * 60 * 1000).toISOString(),
      }),
    ]);
    await loginAsOwner();
    renderTable();

    await screen.findByText("expired-row.example.com");
    const row = screen.getAllByTestId("domain-row")[0];
    const badge = within(row).getByTestId("domain-at-risk");
    expect(badge).toHaveAttribute("data-risk", "expired");
    expect(badge).toHaveTextContent("Expirado");
  });

  it("shows neither indicator for a healthy domain with a far-out expiration (DHM-08)", async () => {
    mockDomainsPage([
      baseDomain({
        id: "dom-healthy",
        hostname: "healthy-row.example.com",
        ns_drift_detected: false,
        expires_at: new Date(Date.now() + 200 * 24 * 60 * 60 * 1000).toISOString(),
      }),
    ]);
    await loginAsOwner();
    renderTable();

    await screen.findByText("healthy-row.example.com");
    const row = screen.getAllByTestId("domain-row")[0];
    expect(within(row).queryByTestId("domain-at-risk")).not.toBeInTheDocument();
  });

  it("shows neither indicator for a domain never health-checked (DHM-08)", async () => {
    mockDomainsPage([
      baseDomain({
        id: "dom-unchecked",
        hostname: "unchecked-row.example.com",
        ns_drift_detected: false,
        expires_at: null,
        last_rdap_check_at: null,
      }),
    ]);
    await loginAsOwner();
    renderTable();

    await screen.findByText("unchecked-row.example.com");
    const row = screen.getAllByTestId("domain-row")[0];
    expect(within(row).queryByTestId("domain-at-risk")).not.toBeInTheDocument();
  });
});
