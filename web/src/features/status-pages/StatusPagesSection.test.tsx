import { describe, it, expect, afterEach } from "vitest";
import { http, HttpResponse, delay } from "msw";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import "../../lib/i18n";
import { AuthProvider } from "../../auth/AuthProvider";
import { TestQueryProvider } from "../../test/queryClient";
import { server } from "../../test/msw/server";
import { apiFetch } from "../../lib/apiClient";
import type { StatusPage } from "../../types/api";
import { StatusPagesSection } from "./StatusPagesSection";

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

afterEach(async () => {
  await apiFetch("/api/auth/logout", { method: "POST" });
});

function renderSection() {
  return render(
    <MemoryRouter>
      <TestQueryProvider>
        <AuthProvider>
          <StatusPagesSection />
        </AuthProvider>
      </TestQueryProvider>
    </MemoryRouter>
  );
}

describe("StatusPagesSection", () => {
  // SKEL-04/05: while /api/status-pages is loading, skeleton rows render
  // (not the old "Carregando…" paragraph as visible content) inside an
  // aria-busy container that still carries the sr-only loading string.
  it("shows skeletons (not the text) while /api/status-pages is loading", async () => {
    server.use(
      http.get("/api/status-pages", async () => {
        await delay("infinite");
        return HttpResponse.json({ items: [], total: 0, page: 1, page_size: 20 });
      }),
    );
    await loginAsOwner();
    renderSection();

    const srText = await screen.findByText("Carregando…");
    expect(srText.className).toContain("sr-only");
    expect(srText.closest('[aria-busy="true"]')).toBeInTheDocument();
    expect(screen.getAllByTestId("skeleton").length).toBeGreaterThan(0);
  });

  // SKEL-06: once the fetch resolves, skeletons are gone and the real
  // content takes over.
  it("removes the skeletons as soon as /api/status-pages finishes loading", async () => {
    await loginAsOwner();
    renderSection();

    await screen.findByText("Status Beta");
    expect(screen.queryAllByTestId("skeleton")).toHaveLength(0);
  });

  it("creation form has no domain/subdomain fields (SPD-01)", async () => {
    await loginAsOwner();
    renderSection();
    await userEvent.click(await screen.findByRole("button", { name: "Criar status page" }));

    expect(screen.getByLabelText("Nome")).toBeInTheDocument();
    expect(screen.queryByLabelText("Subdomínio")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Domínio")).not.toBeInTheDocument();
  });

  it("row for a page with no domain renders without a broken URL (no 'https://null')", async () => {
    await loginAsOwner();
    renderSection();

    await userEvent.click(await screen.findByRole("button", { name: "Criar status page" }));
    await userEvent.type(screen.getByLabelText("Nome"), "Página Sem Domínio Section Test");
    await userEvent.click(screen.getByRole("button", { name: "Criar" }));

    await waitFor(() => expect(screen.queryByLabelText("Nome")).not.toBeInTheDocument());
    expect(await screen.findByText("Página Sem Domínio Section Test")).toBeInTheDocument();
    expect(document.body.textContent).not.toContain("https://null");
    expect(document.body.textContent).not.toContain("undefined");
  });

  it("row for a page with no domain shows a distinct label and a CTA to attach a domain (SPD-12)", async () => {
    await loginAsOwner();
    renderSection();

    await userEvent.click(await screen.findByRole("button", { name: "Criar status page" }));
    await userEvent.type(screen.getByLabelText("Nome"), "Página SPD-12 Section Test");
    await userEvent.click(screen.getByRole("button", { name: "Criar" }));

    await waitFor(() => expect(screen.queryByLabelText("Nome")).not.toBeInTheDocument());
    expect(await screen.findByText("Página SPD-12 Section Test")).toBeInTheDocument();
    expect(screen.getAllByText("Sem domínio configurado").length).toBeGreaterThan(0);
    expect(screen.getAllByRole("link", { name: "Anexar domínio" }).length).toBeGreaterThan(0);
  });

  it("row for a page with domain attached + draft shows the pending DNS/certificate label, distinct from 'no domain' (SPD-13)", async () => {
    await loginAsOwner();
    renderSection();

    // sp-2 (seeded fixture): domain_id set, state "draft".
    expect(await screen.findByText("Status Beta")).toBeInTheDocument();
    expect(screen.getByText("Aguardando validação de DNS/certificado")).toBeInTheDocument();
    expect(screen.queryByText("Emitindo certificado")).not.toBeInTheDocument();
    expect(screen.queryByText("Sem domínio configurado")).not.toBeInTheDocument();
  });

  it("published/tls_failed pages keep their usual labels in the list (SPD-14)", async () => {
    await loginAsOwner();
    renderSection();

    // sp-1/sp-4 (seeded fixtures): state "published".
    expect((await screen.findAllByText("Publicada")).length).toBeGreaterThan(0);
    // sp-3 (seeded fixture): state "tls_failed".
    expect(screen.getByText("Falha")).toBeInTheDocument();
    expect(
      screen.getByText("Falha ao validar propriedade do domínio via DNS-01.")
    ).toBeInTheDocument();
    expect(screen.queryByText("Emitindo certificado")).not.toBeInTheDocument();
  });

  it("'published' row with null domain_id/subdomain (defended shape, never produced by the real flow) doesn't render a broken URL or throw (mutant #5)", async () => {
    await loginAsOwner();
    // MarkPublished only marks a page "published" via a JOIN by hostname
    // that requires a non-null domain_id, so this combination never occurs
    // through the app's real flow - but the
    // `if (!domain_id || !subdomain) return null` guard in publicUrl()
    // exists as a defense. This test forces this impossible-but-defended
    // fixture via an MSW override to prove the guard actually works
    // (without it, the mutant that removes the guard survives: no other
    // test reaches publicUrl() with null domain_id/subdomain inside the
    // "published" branch).
    const impossiblePublished: StatusPage = {
      id: "sp-impossible-published",
      name: "Página Published Sem Domínio (impossível)",
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

    renderSection();

    expect(await screen.findByText("Página Published Sem Domínio (impossível)")).toBeInTheDocument();
    expect(screen.getByText("Publicada")).toBeInTheDocument();
    expect(document.body.textContent).not.toContain("https://null");
    expect(document.body.textContent).not.toContain("undefined");
    expect(screen.queryByRole("link", { name: /^https:\/\//i })).not.toBeInTheDocument();
  });
});
