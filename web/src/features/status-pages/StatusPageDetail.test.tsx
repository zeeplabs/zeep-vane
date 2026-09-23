import { describe, it, expect, afterEach, vi } from "vitest";
import { http, HttpResponse, delay } from "msw";
import { render, screen, act } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import i18n from "../../lib/i18n";
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
  // SKEL-04/05: while /api/status-pages is loading, a skeleton
  // (header + content blocks) renders instead of the old "Carregando…"
  // paragraph as visible content, inside an aria-busy container that
  // still carries the sr-only loading string.
  it("shows skeletons (not the text) while the status page is loading", async () => {
    await loginAsOwner();
    server.use(
      http.get("/api/status-pages", async () => {
        await delay("infinite");
        return HttpResponse.json({ items: [], total: 0, page: 1, page_size: 20 });
      }),
    );
    renderDetail("sp-1");

    const srText = await screen.findByText("Carregando…");
    expect(srText.className).toContain("sr-only");
    expect(srText.closest('[aria-busy="true"]')).toBeInTheDocument();
    expect(screen.getAllByTestId("skeleton").length).toBeGreaterThan(0);
  });

  // SKEL-06: once the fetch resolves, skeletons are gone and the real
  // editor content takes over.
  it("removes the skeletons as soon as the status page finishes loading", async () => {
    await loginAsOwner();
    renderDetail("sp-1");

    expect(await screen.findByText("Publicada")).toBeInTheDocument();
    expect(screen.queryAllByTestId("skeleton")).toHaveLength(0);
  });

  it("published state shows the public URL, the preview link, and does no extra polling", async () => {
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

  it("tls_failed state shows the failure reason, the preview link, and does no extra polling", async () => {
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

  it("no domain (domain_id null) shows a distinct label and an attach-domain button (SPD-12)", async () => {
    await loginAsOwner();
    const page = await createDomainlessPage("Detail Sem Domínio Test");
    renderDetail(page.id);

    expect(await screen.findByText("Sem domínio configurado")).toBeInTheDocument();
    expect(screen.queryByText("Aguardando validação de DNS/certificado")).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Pré-visualizar página pública" })).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Anexar domínio" }));
    expect(await screen.findByLabelText("Domínio")).toBeInTheDocument();
  });

  it("domain attached + draft shows the pending DNS/certificate label, distinct from 'no domain' (SPD-13)", async () => {
    await loginAsOwner();
    renderDetail("sp-2");

    expect(await screen.findByText("Aguardando validação de DNS/certificado")).toBeInTheDocument();
    expect(screen.queryByText("Sem domínio configurado")).not.toBeInTheDocument();
    expect(screen.queryByText("Emitindo certificado")).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Pré-visualizar página pública" })).toBeInTheDocument();
  });

  it("owner with a pending_tls page sees the DNS verification panel and can trigger the check", async () => {
    await loginAsOwner();
    renderDetail("sp-2");
    await screen.findByText("Aguardando validação de DNS/certificado");

    expect(screen.getByText("Configuração DNS")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Verificar DNS/certificado" })).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Verificar DNS/certificado" }));

    // The verify-domain mock publishes the page (see handlers.ts) - the panel
    // disappears as soon as the mutation invalidates the query and the state
    // becomes "published" (there's no reliable, non-racy way to observe the
    // intermediate result, since it unmounts at that same instant).
    expect(await screen.findByText("Publicada")).toBeInTheDocument();
    expect(screen.queryByText("Configuração DNS")).not.toBeInTheDocument();
  });

  it("copy CNAME button copies the displayed value to the clipboard (SPD-10)", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.assign(navigator, { clipboard: { writeText } });

    await loginAsOwner();
    renderDetail("sp-2");
    await screen.findByText("Aguardando validação de DNS/certificado");

    expect(await screen.findByText("203.0.113.10")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Copiar valor do CNAME" }));

    expect(writeText).toHaveBeenCalledWith("203.0.113.10");
  });

  it("verification result with wrong DNS/invalid certificate is shown without publishing the page", async () => {
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
    // Did not publish - panel stays visible for the admin to try again.
    expect(screen.getByText("Configuração DNS")).toBeInTheDocument();
    expect(screen.queryByText("Publicada")).not.toBeInTheDocument();
  });

  it("viewer does not see the DNS verification panel (its endpoints are write-role-gated)", async () => {
    await loginAs("viewer@vane.app");
    renderDetail("sp-2");

    expect(await screen.findByText("Aguardando validação de DNS/certificado")).toBeInTheDocument();
    expect(screen.queryByText("Configuração DNS")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Verificar DNS/certificado" })).not.toBeInTheDocument();
  });

  it("'published' state with null domain_id/subdomain (defended shape, never produced by the real flow) doesn't render a broken URL or throw (mutant #5)", async () => {
    await loginAsOwner();
    // Same reasoning as StatusPagesSection.test.tsx: MarkPublished only
    // marks a page "published" via a JOIN by hostname that requires a
    // non-null domain_id, so this never happens through the real flow - but
    // the `if (!domainId || !subdomain) return null` guard in publicUrl()
    // exists as a defense. Fixture forced via MSW override.
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

  it("shows linked services checked and the rest unchecked", async () => {
    await loginAsOwner();
    renderDetail("sp-1");

    expect(await screen.findByText(/^Serviços vinculados/)).toBeInTheDocument();
    // sp-1 fixture: service_ids = ["svc-1", "svc-2"] ("API pública", "Checkout").
    // Linked services show as checked (aria-pressed) in the "Vinculados" group;
    // the rest show as unchecked in the "Disponíveis" group (SPD-16 redesign) -
    // a button checklist with a visual checkbox, not <input type="checkbox">.
    expect(screen.getByRole("button", { name: "API pública" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("button", { name: "Checkout" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("button", { name: "Notificações" })).toHaveAttribute("aria-pressed", "false");
    expect(screen.getByRole("button", { name: "Fila de processamento" })).toHaveAttribute("aria-pressed", "false");
  });

  it("owner toggles a service and saves, persisting the new set via PATCH (SPD-15)", async () => {
    await loginAsOwner();
    renderDetail("sp-1");
    await screen.findByText(/^Serviços vinculados/);

    const saveButton = screen.getByRole("button", { name: "Salvar serviços" });
    expect(saveButton).toBeDisabled();

    await userEvent.click(screen.getByRole("button", { name: "Notificações" }));
    expect(saveButton).toBeEnabled();

    await userEvent.click(saveButton);

    await vi.waitFor(() => expect(saveButton).toBeDisabled());
    expect(screen.getByRole("button", { name: "API pública" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("button", { name: "Checkout" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("button", { name: "Notificações" })).toHaveAttribute("aria-pressed", "true");

    // Confirms it actually persisted (reflects the server, not just local
    // optimistic state): re-fetches via GET /api/status-pages.
    const reloaded = await apiClient.apiFetch<Page<StatusPage>>("/api/status-pages?page=1");
    const page = reloaded.items.find((p) => p.id === "sp-1");
    expect(page?.service_ids.sort()).toEqual(["svc-1", "svc-2", "svc-3"]);
  });

  it("unchecking all and saving replaces the whole set with empty (replace-all, not incremental)", async () => {
    await loginAsOwner();
    renderDetail("sp-1");
    await screen.findByText(/^Serviços vinculados/);

    await userEvent.click(screen.getByRole("button", { name: "API pública" }));
    await userEvent.click(screen.getByRole("button", { name: "Checkout" }));
    await userEvent.click(screen.getByRole("button", { name: "Salvar serviços" }));

    await vi.waitFor(async () => {
      const reloaded = await apiClient.apiFetch<Page<StatusPage>>("/api/status-pages?page=1");
      expect(reloaded.items.find((p) => p.id === "sp-1")?.service_ids).toEqual([]);
    });
  });

  it("search filters the Disponíveis group without affecting the Vinculados group (SPD-16)", async () => {
    await loginAsOwner();
    renderDetail("sp-1");
    await screen.findByText(/^Serviços vinculados/);

    // sp-1 fixture: linked ("Vinculados") = "API pública", "Checkout"; available
    // ("Disponíveis") = "Notificações", "Fila de processamento".
    await userEvent.type(screen.getByLabelText("Disponíveis"), "notif");

    expect(screen.getByRole("button", { name: "Notificações" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Fila de processamento" })).not.toBeInTheDocument();
    // "Vinculados" is never affected by the filter, even with no match in the searched text.
    expect(screen.getByRole("button", { name: "API pública" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Checkout" })).toBeInTheDocument();
  });

  it("viewer sees the linked services but cannot change them or see the save button", async () => {
    await loginAs("viewer@vane.app");
    renderDetail("sp-1");

    expect(await screen.findByText(/^Serviços vinculados/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "API pública" })).toBeDisabled();
    expect(screen.queryByRole("button", { name: "Salvar serviços" })).not.toBeInTheDocument();
  });

  it("renders in English when the active language is en", async () => {
    await loginAsOwner();
    await i18n.changeLanguage("en");

    try {
      renderDetail("sp-1");

      expect(await screen.findByText(/^Linked services/)).toBeInTheDocument();
      expect(screen.getByText("Available")).toBeInTheDocument();
    } finally {
      await i18n.changeLanguage("pt-BR");
    }
  });
});
