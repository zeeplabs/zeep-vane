import { useState } from "react";
import "../../lib/i18n";
import { describe, it, expect, afterEach } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { server } from "../../test/msw/server";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch } from "../../lib/apiClient";
import { formatDateTime } from "../../lib/formatDate";
import type { Domain, Page } from "../../types/api";
import { DomainsTable } from "./DomainsTable";
import { DomainDetailDrawer } from "./DomainDetailDrawer";

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

/** Harness combining DomainsTable + DomainDetailDrawer under the same
 * QueryClient - needed to exercise DSP-07 end to end (the delete's
 * cache invalidation removing the row from the table). */
function TableAndDrawer() {
  const [selected, setSelected] = useState<Domain | null>(null);
  return (
    <>
      <DomainsTable onSelect={setSelected} />
      <DomainDetailDrawer domain={selected} onClose={() => setSelected(null)} />
    </>
  );
}

function renderHarness() {
  return render(
    <TestQueryProvider>
      <TableAndDrawer />
    </TestQueryProvider>
  );
}

describe("DomainDetailDrawer", () => {
  it("renders status, error banner (last_error) and the TXT instruction block for a custom domain, with no SSL card (DSP-05, DATV-06)", async () => {
    mockDomainsPage([
      baseDomain({
        id: "dom-error",
        hostname: "error.example.com",
        status: "error",
        last_error: "DNS not resolved: no record found for this hostname",
      }),
    ]);
    await loginAsOwner();
    renderHarness();

    await userEvent.click(await screen.findByText("error.example.com"));

    expect(await screen.findByText("DNS not resolved: no record found for this hostname")).toBeInTheDocument();
    expect(screen.getByTestId("domain-txt")).toBeInTheDocument();
    expect(screen.getByText("_vane-verify.error.example.com")).toBeInTheDocument();
    expect(screen.getByText("dom-fixture-token")).toBeInTheDocument();
    // DATV-06: the SSL status card is gone entirely.
    expect(screen.queryByText("SSL/TLS")).not.toBeInTheDocument();
    expect(screen.queryByText("domains.detail.sslLabel")).not.toBeInTheDocument();
    expect(screen.getAllByText("Erro").length).toBeGreaterThan(0);
  });

  it("shows the TXT record name and value for an unverified domain (DATV-02)", async () => {
    mockDomainsPage([
      baseDomain({
        id: "dom-txt",
        hostname: "txt.example.com",
        status: "pending",
        verified_at: null,
        verification_txt_value: "abc123token",
      }),
    ]);
    await loginAsOwner();
    renderHarness();

    await userEvent.click(await screen.findByText("txt.example.com"));

    const block = await screen.findByTestId("domain-txt");
    expect(within(block).getByText("_vane-verify.txt.example.com")).toBeInTheDocument();
    expect(within(block).getByText("abc123token")).toBeInTheDocument();
  });

  it("'Verificar novamente' calls useRecheckDomain and the drawer reflects the returned new state (DSP-06)", async () => {
    const domain = baseDomain({
      id: "dom-recheck",
      hostname: "recheck.example.com",
      status: "pending",
      verified_at: null,
    });
    mockDomainsPage([domain]);
    server.use(
      http.post("/api/domains/:id/verify", () =>
        HttpResponse.json({ ...domain, status: "verified", verified_at: new Date().toISOString() })
      )
    );
    await loginAsOwner();
    renderHarness();

    await userEvent.click(await screen.findByText("recheck.example.com"));
    await userEvent.click(screen.getByRole("button", { name: "Verificar novamente" }));

    await waitFor(() => expect(within(screen.getByRole("dialog")).getByText("Verificado")).toBeInTheDocument());
  });

  it("successful 'Remover domínio' closes the drawer and removes the row from the table (DSP-07)", async () => {
    // Uses the real (unmocked) MSW domainsState-backed handlers rather than
    // a static server.use() override for GET /api/domains - the row's
    // disappearance depends on the DELETE handler's mutation of
    // domainsState actually being visible to a refetched GET, which a
    // fixed-list override would never reflect.
    await loginAsOwner();
    await apiFetch<Domain>("/api/domains", {
      method: "POST",
      body: JSON.stringify({ hostname: "remove.example.com" }),
    });
    renderHarness();

    await userEvent.click(await screen.findByText("remove.example.com"));
    await userEvent.click(screen.getByRole("button", { name: "Remover domínio" }));

    await waitFor(() => expect(screen.queryByText("remove.example.com")).not.toBeInTheDocument());
    expect(screen.queryByRole("button", { name: "Remover domínio" })).not.toBeInTheDocument();
  });

  it("'Remover domínio' with 409 shows an inline error and keeps the drawer open (DSP-08)", async () => {
    mockDomainsPage([baseDomain({ id: "dom-inuse", hostname: "inuse.example.com" })]);
    server.use(
      http.delete("/api/domains/:id", () =>
        HttpResponse.json({ error: "domain is still attached to a status page" }, { status: 409 })
      )
    );
    await loginAsOwner();
    renderHarness();

    await userEvent.click(await screen.findByText("inuse.example.com"));
    await userEvent.click(screen.getByRole("button", { name: "Remover domínio" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("domain is still attached to a status page");
    expect(screen.getByRole("button", { name: "Remover domínio" })).toBeInTheDocument();
  });

  it("renders the health section's expiration, days remaining and registrar (DHM-04)", async () => {
    // 20 days and 6 hours out, so whole-day truncation is deterministically 20.
    const expiresAt = new Date(Date.now() + (20 * 24 + 6) * 60 * 60 * 1000).toISOString();
    mockDomainsPage([
      baseDomain({
        id: "dom-health",
        hostname: "health.example.com",
        expires_at: expiresAt,
        registrar: "GoDaddy.com, LLC",
        expected_ns: ["ns1.example.net"],
        current_ns: ["ns1.example.net"],
        ns_drift_detected: false,
        last_rdap_check_at: new Date(Date.now() - 60 * 60 * 1000).toISOString(),
      }),
    ]);
    await loginAsOwner();
    renderHarness();

    await userEvent.click(await screen.findByText("health.example.com"));

    expect(await screen.findByText("GoDaddy.com, LLC")).toBeInTheDocument();
    expect(screen.getByTestId("expires-at")).toHaveTextContent(formatDateTime(expiresAt, "pt-BR"));
    expect(screen.getByTestId("days-remaining")).toHaveTextContent("20 dias restantes");
    expect(screen.queryByTestId("ns-drift-badge")).not.toBeInTheDocument();
  });

  it("renders NS drift distinctly with expected vs current and a drift badge (DHM-08)", async () => {
    mockDomainsPage([
      baseDomain({
        id: "dom-drift",
        hostname: "drift.example.com",
        expires_at: new Date(Date.now() + 200 * 24 * 60 * 60 * 1000).toISOString(),
        expected_ns: ["ns1.example.net", "ns2.example.net"],
        current_ns: ["ns9.other.net"],
        ns_drift_detected: true,
        last_rdap_check_at: new Date().toISOString(),
      }),
    ]);
    await loginAsOwner();
    renderHarness();

    await userEvent.click(await screen.findByText("drift.example.com"));

    expect(await screen.findByTestId("ns-drift-badge")).toHaveTextContent("Drift de NS");
    expect(screen.getByTestId("expected-ns")).toHaveTextContent("ns1.example.net, ns2.example.net");
    expect(screen.getByTestId("current-ns")).toHaveTextContent("ns9.other.net");
  });

  it("shows the RDAP error indicator without hiding the rest of the drawer (DHM-04)", async () => {
    mockDomainsPage([
      baseDomain({
        id: "dom-rdap-err",
        hostname: "rdap-error.example.com",
        expires_at: null,
        registrar: null,
        last_rdap_check_at: new Date().toISOString(),
        rdap_last_error: "rdap: unexpected status 404",
      }),
    ]);
    await loginAsOwner();
    renderHarness();

    await userEvent.click(await screen.findByText("rdap-error.example.com"));

    expect(await screen.findByTestId("rdap-error")).toHaveTextContent("rdap: unexpected status 404");
    // The rest of the drawer still renders normally alongside the warning.
    expect(screen.getByTestId("domain-health")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Verificar novamente" })).toBeInTheDocument();
  });

  it("renders a not-checked-yet state when the domain has no health data (DHM-04)", async () => {
    mockDomainsPage([
      baseDomain({ id: "dom-nohealth", hostname: "nohealth.example.com", last_rdap_check_at: null }),
    ]);
    await loginAsOwner();
    renderHarness();

    await userEvent.click(await screen.findByText("nohealth.example.com"));

    expect(await screen.findByText("Ainda não verificado")).toBeInTheDocument();
    expect(screen.queryByTestId("days-remaining")).not.toBeInTheDocument();
    expect(screen.queryByTestId("ns-drift-badge")).not.toBeInTheDocument();
  });
});
