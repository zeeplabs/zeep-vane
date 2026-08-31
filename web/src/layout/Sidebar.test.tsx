import { describe, it, expect, afterEach, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import "../lib/i18n";
import { AuthProvider } from "../auth/AuthProvider";
import { Sidebar } from "./Sidebar";
import { apiFetch } from "../lib/apiClient";
import { TestQueryProvider } from "../test/queryClient";

async function loginAs(email: string) {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email, password: "demo1234" }),
  });
}

afterEach(async () => {
  try {
    await apiFetch("/api/auth/logout", { method: "POST" });
  } catch {
    /* ignore */
  }
});

function renderSidebar() {
  return render(
    <TestQueryProvider>
      <MemoryRouter>
        <AuthProvider>
          <Sidebar />
        </AuthProvider>
      </MemoryRouter>
    </TestQueryProvider>
  );
}

describe("Sidebar", () => {
  it("esconde 'Equipe' para non-owner", async () => {
    await loginAs("viewer@vane.app");
    renderSidebar();
    await waitFor(() => expect(screen.getByText("Domínios & Status Pages")).toBeInTheDocument());
    expect(screen.queryByText("Equipe")).not.toBeInTheDocument();
  });

  it("mostra 'Equipe' para owner", async () => {
    await loginAs("owner@vane.app");
    renderSidebar();
    await waitFor(() => expect(screen.getByText("Equipe")).toBeInTheDocument());
  });

  it("mostra link para Serviços apontando pra /services", async () => {
    await loginAs("owner@vane.app");
    renderSidebar();
    const link = await screen.findByRole("link", { name: "Serviços" });
    expect(link).toHaveAttribute("href", "/services");
  });

  it("mostra o controle 'Visualizando como' em DEV", async () => {
    await loginAs("owner@vane.app");
    renderSidebar();
    await waitFor(() => expect(screen.getByRole("radiogroup")).toBeInTheDocument());
  });

  it("esconde o controle 'Visualizando como' fora de DEV", async () => {
    vi.stubEnv("DEV", false);
    await loginAs("owner@vane.app");
    renderSidebar();
    await waitFor(() => expect(screen.getByText("Domínios & Status Pages")).toBeInTheDocument());
    expect(screen.queryByRole("radiogroup")).not.toBeInTheDocument();
    vi.unstubAllEnvs();
  });

  it("mostra nome e e-mail do admin logado acima do botão Sair", async () => {
    await loginAs("owner@vane.app");
    renderSidebar();
    expect(await screen.findByText("Ana Owner")).toBeInTheDocument();
    expect(screen.getByText("owner@vane.app")).toBeInTheDocument();
  });
});
