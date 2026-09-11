import { describe, it, expect, afterEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import "../lib/i18n";
import { AuthProvider } from "../auth/AuthProvider";
import { AppShell } from "./AppShell";
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

function renderShellAt(pathname: string) {
  return render(
    <TestQueryProvider>
      <MemoryRouter initialEntries={[pathname]}>
        <AuthProvider>
          <AppShell>
            <div data-testid="routed-content">conteúdo da rota</div>
          </AppShell>
        </AuthProvider>
      </MemoryRouter>
    </TestQueryProvider>
  );
}

describe("AppShell", () => {
  it("deriva o título do Topbar a partir da rota atual (SHELL-08)", async () => {
    await loginAs("owner@vane.app");
    renderShellAt("/incidents");
    expect(await screen.findByRole("heading", { level: 1, name: "Incidentes" })).toBeInTheDocument();
  });

  it("deriva um título diferente para outra rota", async () => {
    await loginAs("owner@vane.app");
    renderShellAt("/settings");
    expect(await screen.findByRole("heading", { level: 1, name: "Configurações" })).toBeInTheDocument();
  });

  it("PollerBanner mantém a mesma posição relativa ao conteúdo roteado (antes do main)", async () => {
    await loginAs("owner@vane.app");
    const { container } = renderShellAt("/services");
    await screen.findByTestId("routed-content");

    const bannerSlot = container.querySelector('[data-testid="global-banner-slot"]');
    const routedContent = screen.getByTestId("routed-content");
    expect(bannerSlot).not.toBeNull();
    expect(bannerSlot!.compareDocumentPosition(routedContent) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });
});
