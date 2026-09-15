import { useState } from "react";
import { describe, it, expect, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { server } from "../../test/msw/server";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch } from "../../lib/apiClient";
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
    ssl_status: "active",
    verified_at: new Date().toISOString(),
    last_error: null,
    attached_page_name: null,
    attached_page_count: 0,
    ...overrides,
  };
}

/** Harness combinando DomainsTable + DomainDetailDrawer sob o mesmo
 * QueryClient - necessário para exercitar DSP-07 de ponta a ponta (a
 * invalidação de cache do delete removendo a linha da tabela). */
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
  it("renderiza status, banner de erro (last_error), SSL/Verificado e bloco de DNS para domínio custom (DSP-05)", async () => {
    mockDomainsPage([
      baseDomain({
        id: "dom-error",
        hostname: "error.example.com",
        status: "error",
        ssl_status: "error",
        last_error: "DNS not resolved: no record found for this hostname",
      }),
    ]);
    await loginAsOwner();
    renderHarness();

    await userEvent.click(await screen.findByText("error.example.com"));

    expect(await screen.findByText("DNS not resolved: no record found for this hostname")).toBeInTheDocument();
    expect(screen.getByText("Configuração DNS")).toBeInTheDocument();
    expect(screen.getAllByText("Erro").length).toBeGreaterThan(0);
  });

  it("'Verificar novamente' chama useRecheckDomain e o drawer reflete o novo estado retornado (DSP-06)", async () => {
    const domain = baseDomain({
      id: "dom-recheck",
      hostname: "recheck.example.com",
      status: "pending",
      ssl_status: "pending",
      verified_at: null,
    });
    mockDomainsPage([domain]);
    server.use(
      http.post("/api/domains/:id/verify", () =>
        HttpResponse.json({ ...domain, status: "verified", ssl_status: "active", verified_at: new Date().toISOString() })
      )
    );
    await loginAsOwner();
    renderHarness();

    await userEvent.click(await screen.findByText("recheck.example.com"));
    await userEvent.click(screen.getByRole("button", { name: "Verificar novamente" }));

    await waitFor(() => expect(screen.getByText("Ativo")).toBeInTheDocument());
  });

  it("'Remover domínio' bem-sucedido fecha o drawer e remove a linha da tabela (DSP-07)", async () => {
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

  it("'Remover domínio' com 409 mostra erro inline e mantém o drawer aberto (DSP-08)", async () => {
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
});
