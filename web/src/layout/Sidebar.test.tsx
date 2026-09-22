import { describe, it, expect, afterEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { http, HttpResponse } from "msw";
import "../lib/i18n";
import { AuthProvider } from "../auth/AuthProvider";
import { Sidebar } from "./Sidebar";
import { apiFetch } from "../lib/apiClient";
import { server } from "../test/msw/server";
import { TestQueryProvider } from "../test/queryClient";
import { setDeploymentMode, resetDeploymentMode } from "../test/msw/handlers";

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
  resetDeploymentMode();
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
  it("hides 'Usuários' for non-owner", async () => {
    await loginAs("viewer@vane.app");
    renderSidebar();
    await waitFor(() => expect(screen.getByText("Domínios & Status")).toBeInTheDocument());
    expect(screen.queryByText("Usuários")).not.toBeInTheDocument();
  });

  it("shows 'Usuários' for owner", async () => {
    await loginAs("owner@vane.app");
    renderSidebar();
    await waitFor(() => expect(screen.getByText("Usuários")).toBeInTheDocument());
  });

  it("hides 'Configurações' for non-owner", async () => {
    await loginAs("viewer@vane.app");
    renderSidebar();
    await waitFor(() => expect(screen.getByText("Domínios & Status")).toBeInTheDocument());
    expect(screen.queryByText("Configurações")).not.toBeInTheDocument();
  });

  it("shows 'Configurações' for owner (role gate, other side)", async () => {
    await loginAs("owner@vane.app");
    renderSidebar();
    await waitFor(() => expect(screen.getByText("Configurações")).toBeInTheDocument());
  });

  it("shows a link for Serviços monitorados pointing to /services", async () => {
    await loginAs("owner@vane.app");
    renderSidebar();
    const link = await screen.findByRole("link", { name: "Serviços monitorados" });
    expect(link).toHaveAttribute("href", "/services");
  });

  // billing-plans-page BILLPG-01: this reverses the earlier "out of scope"
  // omission - the item now exists as a real (decorative) page.
  // Visible here because the default MSW deploymentMode is saas.
  it("shows 'Planos & Faturamento' in the Organização group, pointing to /billing", async () => {
    await loginAs("owner@vane.app");
    renderSidebar();
    const link = await screen.findByRole("link", { name: "Planos & Faturamento" });
    expect(link).toHaveAttribute("href", "/billing");
  });

  it("shows 'Planos & Faturamento' for non-owner too (no role gate)", async () => {
    await loginAs("viewer@vane.app");
    renderSidebar();
    expect(await screen.findByRole("link", { name: "Planos & Faturamento" })).toBeInTheDocument();
  });

  // 2026-09-16: reverses BILLPG-01's "reachable in any mode" - self_hosted
  // has no plan/upgrade flow to show yet (license-purchase redirect is a
  // future feature), so the item is hidden entirely in that mode.
  it("hides 'Planos & Faturamento' in self_hosted", async () => {
    setDeploymentMode("self_hosted");
    await loginAs("owner@vane.app");
    renderSidebar();
    await waitFor(() => expect(screen.getByText("Domínios & Status")).toBeInTheDocument());
    expect(screen.queryByText("Planos & Faturamento")).not.toBeInTheDocument();
  });

  it("shows 'Planos & Faturamento' in saas", async () => {
    setDeploymentMode("saas");
    await loginAs("owner@vane.app");
    renderSidebar();
    expect(await screen.findByRole("link", { name: "Planos & Faturamento" })).toBeInTheDocument();
  });

  it("collapsed by default (72px), expands on mouseenter and collapses again on mouseleave", async () => {
    await loginAs("owner@vane.app");
    renderSidebar();
    const sidebar = await screen.findByTestId("sidebar");
    expect(sidebar.className).toContain("w-[72px]");

    fireEvent.mouseEnter(sidebar);
    expect(sidebar.className).toContain("w-[240px]");

    fireEvent.mouseLeave(sidebar);
    expect(sidebar.className).toContain("w-[72px]");
  });

  it("with the menu pinned, mouseleave does not collapse the sidebar", async () => {
    await loginAs("owner@vane.app");
    renderSidebar();
    const sidebar = await screen.findByTestId("sidebar");

    await userEvent.click(screen.getByRole("button", { name: "Fixar menu" }));
    expect(sidebar.className).toContain("w-[240px]");

    fireEvent.mouseEnter(sidebar);
    fireEvent.mouseLeave(sidebar);
    expect(sidebar.className).toContain("w-[240px]");
  });

  it("highlights the current route's nav item with the accent background/text", async () => {
    await loginAs("owner@vane.app");
    renderSidebar("/services");
    const link = await screen.findByRole("link", { name: "Serviços monitorados" });
    expect(link.className).toContain("text-accent");
    expect(link.className).toContain("bg-[rgba(90,70,199,0.08)]");
  });

  it("TenantSwitcher (>1 membership) renders at the top of the sidebar, above the nav groups", async () => {
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
    const { getByTestId } = renderSidebar();
    // Name/badge only render expanded (collapsed rail shows just the
    // avatar, matching the handoff) - hover to expand before querying.
    fireEvent.mouseEnter(getByTestId("sidebar"));

    const trigger = await screen.findByRole("button", { name: /Acme Corp/ });
    const nav = await screen.findByText("Serviços monitorados");
    // compareDocumentPosition bit 4 (DOCUMENT_POSITION_FOLLOWING) confirms
    // the nav item comes after the tenant switcher trigger in DOM order.
    expect(trigger.compareDocumentPosition(nav) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it("'Fixar menu' persists in localStorage across mounts", async () => {
    await loginAs("owner@vane.app");
    const { unmount } = renderSidebar();
    await screen.findByTestId("sidebar");
    await userEvent.click(screen.getByRole("button", { name: "Fixar menu" }));
    unmount();

    renderSidebar();
    const sidebar = await screen.findByTestId("sidebar");
    expect(sidebar.className).toContain("w-[240px]");
  });

  it("shows 'Visão geral' as the first item, above the nav groups (OVW-15)", async () => {
    await loginAs("owner@vane.app");
    renderSidebar();

    const overviewLink = await screen.findByRole("link", { name: "Visão geral" });
    const servicesLink = screen.getByRole("link", { name: "Serviços monitorados" });
    expect(overviewLink).toHaveAttribute("href", "/overview");
    // DOM order: the standalone overview item precedes the first grouped item.
    expect(overviewLink.compareDocumentPosition(servicesLink) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it("'Visão geral' stays active on both / and /overview (OVW-16)", async () => {
    await loginAs("owner@vane.app");
    const { unmount } = renderSidebar("/");
    const atRoot = await screen.findByRole("link", { name: "Visão geral" });
    expect(atRoot.className).toContain("text-accent");
    unmount();

    renderSidebar("/overview");
    const atOverview = await screen.findByRole("link", { name: "Visão geral" });
    expect(atOverview.className).toContain("text-accent");
  });
});
