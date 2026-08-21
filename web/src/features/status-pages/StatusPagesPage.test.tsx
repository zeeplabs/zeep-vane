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
    expect(await screen.findByText("Publicada")).toBeInTheDocument();
    expect(screen.getByText("Emitindo certificado")).toBeInTheDocument();
    expect(screen.getByText("Falha")).toBeInTheDocument();
  });

  it("viewer não vê o formulário de criação", async () => {
    await loginAs("viewer@vane.app");
    renderPage();
    await screen.findByText("Publicada");
    expect(screen.queryByRole("button", { name: "Criar status page" })).not.toBeInTheDocument();
  });

  it("criação exibe a página nova em estado emitindo certificado", async () => {
    await loginAs("owner@vane.app");
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "Criar status page" }));
    await userEvent.type(screen.getByLabelText("Nome"), "Status Nova Empresa");
    await userEvent.type(screen.getByLabelText("Subdomínio"), "novaempresa");
    await userEvent.selectOptions(screen.getByLabelText("Domínio"), "status.acme.com");
    await userEvent.click(screen.getByRole("button", { name: "Criar" }));

    await waitFor(() => expect(screen.queryByLabelText("Nome")).not.toBeInTheDocument());
    expect(await screen.findByText("Status Nova Empresa")).toBeInTheDocument();
    const tags = screen.getAllByText("Emitindo certificado");
    expect(tags.length).toBeGreaterThan(0);
  });
});
