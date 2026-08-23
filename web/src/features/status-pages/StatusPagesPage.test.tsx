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
  it("lista status pages com tags de estado mapeadas", async () => {
    await loginAs("owner@vane.app");
    renderPage();
    expect((await screen.findAllByText("Publicada")).length).toBeGreaterThan(0);
    // sp-2 tem domain_id preenchido e state "draft" (SPD-13): label de
    // DNS/certificado pendente, não mais o antigo "Emitindo certificado"
    // ambíguo (removido pela correção do Gap 1 do Verifier).
    expect(screen.getByText("Aguardando validação de DNS/certificado")).toBeInTheDocument();
    expect(screen.queryByText("Emitindo certificado")).not.toBeInTheDocument();
    expect(screen.getByText("Falha")).toBeInTheDocument();
  });

  it("viewer não vê o formulário de criação", async () => {
    await loginAs("viewer@vane.app");
    renderPage();
    await screen.findAllByText("Publicada");
    expect(screen.queryByRole("button", { name: "Criar status page" })).not.toBeInTheDocument();
  });

  it("criação não tem campos de domínio e a página nova nasce sem domínio, com label distinto de 'sem domínio configurado' (SPD-01/SPD-12)", async () => {
    await loginAs("owner@vane.app");
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "Criar status page" }));
    expect(screen.queryByLabelText("Subdomínio")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Domínio")).not.toBeInTheDocument();
    await userEvent.type(screen.getByLabelText("Nome"), "Status Nova Empresa");
    await userEvent.click(screen.getByRole("button", { name: "Criar" }));

    await waitFor(() => expect(screen.queryByLabelText("Nome")).not.toBeInTheDocument());
    expect(await screen.findByText("Status Nova Empresa")).toBeInTheDocument();
    // Página recém-criada não tem domínio (domain_id: null): a lista deve
    // mostrar o label distinto exigido pela spec (SPD-12), nunca o antigo
    // "Emitindo certificado" ambíguo que a spec proíbe para esse caso.
    const labels = screen.getAllByText("Sem domínio configurado");
    expect(labels.length).toBeGreaterThan(0);
    expect(screen.queryByText("Emitindo certificado")).not.toBeInTheDocument();
    expect(screen.getByText("—")).toBeInTheDocument();
  });
});
