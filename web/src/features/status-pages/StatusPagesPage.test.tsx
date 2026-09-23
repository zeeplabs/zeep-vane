import { describe, it, expect, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import "../../lib/i18n";
import { AuthProvider } from "../../auth/AuthProvider";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch } from "../../lib/apiClient";
import { StatusPagesPage } from "./StatusPagesPage";

async function loginAs(email: string) {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email, password: "demo1234" }),
  });
}

afterEach(async () => {
  await apiFetch("/api/auth/logout", { method: "POST" });
});

function renderPage() {
  return render(
    <MemoryRouter>
      <TestQueryProvider>
        <AuthProvider>
          <StatusPagesPage />
        </AuthProvider>
      </TestQueryProvider>
    </MemoryRouter>
  );
}

describe("StatusPagesPage", () => {
  it("lists status pages with mapped state tags", async () => {
    await loginAs("owner@vane.app");
    renderPage();
    expect((await screen.findAllByText("Publicada")).length).toBeGreaterThan(0);
    // sp-2 has domain_id set and state "draft" (SPD-13): pending
    // DNS/certificate label, no longer the old ambiguous "Emitindo
    // certificado" (removed by the Verifier's Gap 1 fix).
    expect(screen.getByText("Aguardando validação de DNS/certificado")).toBeInTheDocument();
    expect(screen.queryByText("Emitindo certificado")).not.toBeInTheDocument();
    expect(screen.getByText("Falha")).toBeInTheDocument();
  });

  it("viewer does not see the creation form", async () => {
    await loginAs("viewer@vane.app");
    renderPage();
    await screen.findAllByText("Publicada");
    expect(screen.queryByRole("button", { name: "Criar status page" })).not.toBeInTheDocument();
  });

  it("creation has no domain fields and the new page starts with no domain, with a label distinct from 'sem domínio configurado' (SPD-01/SPD-12)", async () => {
    await loginAs("owner@vane.app");
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "Criar status page" }));
    expect(screen.queryByLabelText("Subdomínio")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Domínio")).not.toBeInTheDocument();
    await userEvent.type(screen.getByLabelText("Nome"), "Status Nova Empresa");
    await userEvent.click(screen.getByRole("button", { name: "Criar" }));

    await waitFor(() => expect(screen.queryByLabelText("Nome")).not.toBeInTheDocument());
    expect(await screen.findByText("Status Nova Empresa")).toBeInTheDocument();
    // Newly created page has no domain (domain_id: null): the list must show
    // the distinct label required by the spec (SPD-12), never the old
    // ambiguous "Emitindo certificado" the spec forbids for this case.
    const labels = screen.getAllByText("Sem domínio configurado");
    expect(labels.length).toBeGreaterThan(0);
    expect(screen.queryByText("Emitindo certificado")).not.toBeInTheDocument();
    expect(screen.getByText("—")).toBeInTheDocument();
  });
});
