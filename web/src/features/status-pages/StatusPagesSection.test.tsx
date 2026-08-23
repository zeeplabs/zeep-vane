import { describe, it, expect, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import "../../lib/i18n";
import { AuthProvider } from "../../auth/AuthProvider";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch } from "../../lib/apiClient";
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
  it("formulário de criação não tem campos de domínio/subdomínio (SPD-01)", async () => {
    await loginAsOwner();
    renderSection();
    await userEvent.click(await screen.findByRole("button", { name: "Criar status page" }));

    expect(screen.getByLabelText("Nome")).toBeInTheDocument();
    expect(screen.queryByLabelText("Subdomínio")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Domínio")).not.toBeInTheDocument();
  });

  it("linha de uma página sem domínio renderiza sem URL quebrada (sem 'https://null')", async () => {
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

  it("linha de uma página sem domínio mostra label distinto e CTA pra anexar domínio (SPD-12)", async () => {
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

  it("linha de uma página com domínio anexado + draft mostra label de DNS/certificado pendente, distinto do 'sem domínio' (SPD-13)", async () => {
    await loginAsOwner();
    renderSection();

    // sp-2 (fixture seedada): domain_id preenchido, state "draft".
    expect(await screen.findByText("Status Beta")).toBeInTheDocument();
    expect(screen.getByText("Aguardando validação de DNS/certificado")).toBeInTheDocument();
    expect(screen.queryByText("Emitindo certificado")).not.toBeInTheDocument();
    expect(screen.queryByText("Sem domínio configurado")).not.toBeInTheDocument();
  });

  it("páginas published/tls_failed mantêm os labels de sempre na lista (SPD-14)", async () => {
    await loginAsOwner();
    renderSection();

    // sp-1/sp-4 (fixtures seedadas): state "published".
    expect((await screen.findAllByText("Publicada")).length).toBeGreaterThan(0);
    // sp-3 (fixture seedada): state "tls_failed".
    expect(screen.getByText("Falha")).toBeInTheDocument();
    expect(
      screen.getByText("Falha ao validar propriedade do domínio via DNS-01.")
    ).toBeInTheDocument();
    expect(screen.queryByText("Emitindo certificado")).not.toBeInTheDocument();
  });
});
