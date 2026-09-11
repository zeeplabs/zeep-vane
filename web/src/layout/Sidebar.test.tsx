import { describe, it, expect, afterEach, vi } from "vitest";
import { render, screen, waitFor, fireEvent, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { http, HttpResponse } from "msw";
import "../lib/i18n";
import { AuthProvider } from "../auth/AuthProvider";
import { Sidebar } from "./Sidebar";
import { apiFetch } from "../lib/apiClient";
import { server } from "../test/msw/server";
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
  window.localStorage.clear();
});

function renderSidebar(initialPath = "/") {
  return render(
    <TestQueryProvider>
      <MemoryRouter initialEntries={[initialPath]}>
        <AuthProvider>
          <Sidebar />
        </AuthProvider>
      </MemoryRouter>
    </TestQueryProvider>
  );
}

describe("Sidebar", () => {
  it("esconde 'Usuários' para non-owner", async () => {
    await loginAs("viewer@vane.app");
    renderSidebar();
    await waitFor(() => expect(screen.getByText("Domínios & Status")).toBeInTheDocument());
    expect(screen.queryByText("Usuários")).not.toBeInTheDocument();
  });

  it("mostra 'Usuários' para owner", async () => {
    await loginAs("owner@vane.app");
    renderSidebar();
    await waitFor(() => expect(screen.getByText("Usuários")).toBeInTheDocument());
  });

  it("esconde 'Configurações' para non-owner", async () => {
    await loginAs("viewer@vane.app");
    renderSidebar();
    await waitFor(() => expect(screen.getByText("Domínios & Status")).toBeInTheDocument());
    expect(screen.queryByText("Configurações")).not.toBeInTheDocument();
  });

  it("mostra 'Configurações' para owner (role gate, outro lado)", async () => {
    await loginAs("owner@vane.app");
    renderSidebar();
    await waitFor(() => expect(screen.getByText("Configurações")).toBeInTheDocument());
  });

  it("mostra link para Serviços monitorados apontando pra /services", async () => {
    await loginAs("owner@vane.app");
    renderSidebar();
    const link = await screen.findByRole("link", { name: "Serviços monitorados" });
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
    await waitFor(() => expect(screen.getByText("Domínios & Status")).toBeInTheDocument());
    expect(screen.queryByRole("radiogroup")).not.toBeInTheDocument();
    vi.unstubAllEnvs();
  });

  it("mostra nome e e-mail do admin logado acima do botão Sair", async () => {
    await loginAs("owner@vane.app");
    renderSidebar();
    expect(await screen.findByText("Ana Owner")).toBeInTheDocument();
    expect(screen.getByText("owner@vane.app")).toBeInTheDocument();
  });

  it("omite 'Planos & Faturamento' do grupo Organização (fora de escopo)", async () => {
    await loginAs("owner@vane.app");
    renderSidebar();
    await waitFor(() => expect(screen.getByText("Usuários")).toBeInTheDocument());
    expect(screen.queryByText("Planos & Faturamento")).not.toBeInTheDocument();
  });

  it("colapsada por padrão (72px), expande com mouseenter e recolapsa com mouseleave", async () => {
    await loginAs("owner@vane.app");
    renderSidebar();
    const sidebar = await screen.findByTestId("sidebar");
    expect(sidebar.className).toContain("w-[72px]");

    fireEvent.mouseEnter(sidebar);
    expect(sidebar.className).toContain("w-[240px]");

    fireEvent.mouseLeave(sidebar);
    expect(sidebar.className).toContain("w-[72px]");
  });

  it("com o menu fixado, mouseleave não recolapsa a sidebar", async () => {
    await loginAs("owner@vane.app");
    renderSidebar();
    const sidebar = await screen.findByTestId("sidebar");

    await userEvent.click(screen.getByRole("button", { name: "Fixar menu" }));
    expect(sidebar.className).toContain("w-[240px]");

    fireEvent.mouseEnter(sidebar);
    fireEvent.mouseLeave(sidebar);
    expect(sidebar.className).toContain("w-[240px]");
  });

  it("destaca o item de nav da rota atual com o fundo/texto de acento", async () => {
    await loginAs("owner@vane.app");
    renderSidebar("/services");
    const link = await screen.findByRole("link", { name: "Serviços monitorados" });
    expect(link.className).toContain("text-accent");
    expect(link.className).toContain("bg-[rgba(90,70,199,0.08)]");
  });

  it("botão de logout da sidebar abre o LogoutConfirmDialog e confirmar chama logout", async () => {
    await loginAs("owner@vane.app");
    renderSidebar();
    await userEvent.click(await screen.findByRole("button", { name: "Sair" }));
    expect(await screen.findByText("Sair do painel")).toBeInTheDocument();

    const dialog = screen.getByRole("dialog");
    await userEvent.click(within(dialog).getByRole("button", { name: "Sair" }));
    await waitFor(() => expect(screen.queryByText("Ana Owner")).not.toBeInTheDocument());
  });

  it("TenantSwitcher (>1 membership) renderiza no topo da sidebar, acima dos grupos de nav", async () => {
    server.use(
      http.get("/api/auth/me", () =>
        HttpResponse.json({
          id: "admin-1",
          email: "owner@vane.app",
          name: "Ana Owner",
          role: "owner",
          active_tenant_id: "tenant-1",
          memberships: [
            { tenant_id: "tenant-1", role: "owner", name: "Acme Corp", plan_tier: "scale" },
            { tenant_id: "tenant-2", role: "operator", name: "Beta Inc", plan_tier: "" },
          ],
        })
      )
    );
    await loginAs("owner@vane.app");
    renderSidebar();

    const trigger = await screen.findByRole("button", { name: /Acme Corp/ });
    const nav = await screen.findByText("Serviços monitorados");
    // compareDocumentPosition bit 4 (DOCUMENT_POSITION_FOLLOWING) confirms
    // the nav item comes after the tenant switcher trigger in DOM order.
    expect(trigger.compareDocumentPosition(nav) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it("'Fixar menu' persiste em localStorage entre montagens", async () => {
    await loginAs("owner@vane.app");
    const { unmount } = renderSidebar();
    await screen.findByTestId("sidebar");
    await userEvent.click(screen.getByRole("button", { name: "Fixar menu" }));
    unmount();

    renderSidebar();
    const sidebar = await screen.findByTestId("sidebar");
    expect(sidebar.className).toContain("w-[240px]");
  });
});
