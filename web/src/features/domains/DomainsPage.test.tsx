import { describe, it, expect, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import "../../lib/i18n";
import { AuthProvider } from "../../auth/AuthProvider";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch } from "../../lib/apiClient";
import { DomainsPage } from "./DomainsPage";

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
          <DomainsPage />
        </AuthProvider>
      </TestQueryProvider>
    </MemoryRouter>
  );
}

describe("DomainsPage", () => {
  it("lista domínios com hostname e data de cadastro", async () => {
    await loginAs("owner@vane.app");
    renderPage();
    expect(await screen.findByText("status.acme.com")).toBeInTheDocument();
    expect(screen.getAllByText("Cadastrado em").length).toBeGreaterThan(0);
  });

  it("viewer não vê o formulário de cadastro", async () => {
    await loginAs("viewer@vane.app");
    renderPage();
    await screen.findByText("status.acme.com");
    expect(screen.queryByRole("button", { name: "Adicionar domínio" })).not.toBeInTheDocument();
  });

  it("cadastro duplicado exibe erro exato sem criar linha nova", async () => {
    await loginAs("owner@vane.app");
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "Adicionar domínio" }));
    await userEvent.type(screen.getByLabelText("Hostname"), "status.acme.com");
    await userEvent.click(screen.getByRole("button", { name: "Salvar" }));

    // Texto do backend real (I13) - inglês, gap de i18n já identificado em
    // LoginPage.test.tsx (I7), não corrigido aqui (fora de escopo).
    expect(await screen.findByText("hostname already registered")).toBeInTheDocument();
    expect(screen.getAllByText("status.acme.com")).toHaveLength(1); // continua só a linha original, não duplicou
  });

  it("cadastro novo aparece na tabela", async () => {
    await loginAs("owner@vane.app");
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "Adicionar domínio" }));
    await userEvent.type(screen.getByLabelText("Hostname"), "status.novo-dominio.com");
    await userEvent.click(screen.getByRole("button", { name: "Salvar" }));

    await waitFor(() => expect(screen.queryByLabelText("Hostname")).not.toBeInTheDocument());
    expect(await screen.findByText("status.novo-dominio.com")).toBeInTheDocument();
  });
});
