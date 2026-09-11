import { describe, it, expect, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { http, HttpResponse } from "msw";
import "../lib/i18n";
import { AuthProvider, useAuth } from "../auth/AuthProvider";
import { TenantSwitcher } from "./TenantSwitcher";
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
});

// Mirrors the real backend's meResponse shape post-enrichment (SHELL-20/21):
// each membership carries name/plan_tier alongside tenant_id/role.
function useMultiMembershipMe(memberships: Array<{ tenant_id: string; role: string; name: string; plan_tier: string }>) {
  let activeTenantId = memberships[0]?.tenant_id ?? "";
  server.use(
    http.get("/api/auth/me", () =>
      HttpResponse.json({
        id: "admin-1",
        email: "owner@vane.app",
        name: "Ana Owner",
        role: "owner",
        active_tenant_id: activeTenantId,
        memberships,
      })
    ),
    http.post("/api/auth/switch-tenant", async ({ request }) => {
      const body = (await request.json()) as { tenant_id?: string };
      activeTenantId = body.tenant_id ?? activeTenantId;
      return HttpResponse.json({ token: "msw-token-admin-1", tenant_id: activeTenantId });
    })
  );
}

function renderSwitcher() {
  return render(
    <TestQueryProvider>
      <MemoryRouter>
        <AuthProvider>
          <TenantSwitcher />
        </AuthProvider>
      </MemoryRouter>
    </TestQueryProvider>
  );
}

// AdminEmailProbe renders once the auth boot fetch resolves, giving the
// "renders null" test a positive signal to wait on instead of asserting an
// absence that would trivially hold even before boot finishes.
function AdminEmailProbe() {
  const { admin } = useAuth();
  return admin ? <span data-testid="admin-email">{admin.email}</span> : null;
}

describe("TenantSwitcher", () => {
  it("renderiza null para usuário com 1 único membership (SHELL-10)", async () => {
    await loginAs("owner@vane.app");
    const { container } = render(
      <TestQueryProvider>
        <MemoryRouter>
          <AuthProvider>
            <AdminEmailProbe />
            <TenantSwitcher />
          </AuthProvider>
        </MemoryRouter>
      </TestQueryProvider>
    );

    await screen.findByTestId("admin-email");
    expect(container.querySelector("[aria-haspopup]")).not.toBeInTheDocument();
  });

  it("lista todos os memberships com nome e badge de plano, tenant ativo marcado (SHELL-11)", async () => {
    useMultiMembershipMe([
      { tenant_id: "tenant-1", role: "owner", name: "Acme Corp", plan_tier: "scale" },
      { tenant_id: "tenant-2", role: "operator", name: "Beta Inc", plan_tier: "" },
    ]);
    await loginAs("owner@vane.app");
    renderSwitcher();

    await screen.findByText("Acme Corp");
    await userEvent.click(screen.getByRole("button", { name: /Acme Corp/ }));

    const acmeOption = screen.getByRole("option", { name: /Acme Corp/ });
    const betaOption = screen.getByRole("option", { name: /Beta Inc/ });
    expect(acmeOption).toHaveAttribute("aria-selected", "true");
    expect(betaOption).toHaveAttribute("aria-selected", "false");
    expect(acmeOption).toHaveTextContent("scale");
    // plan_tier vazio mostra a pill "Free" (spec.md SHELL-21 / design.md Error Handling).
    expect(betaOption).toHaveTextContent("Free");
  });

  it("selecionar outro tenant chama switchTenant e atualiza o tenant ativo (SHELL-11)", async () => {
    useMultiMembershipMe([
      { tenant_id: "tenant-1", role: "owner", name: "Acme Corp", plan_tier: "scale" },
      { tenant_id: "tenant-2", role: "operator", name: "Beta Inc", plan_tier: "" },
    ]);
    await loginAs("owner@vane.app");
    renderSwitcher();

    await screen.findByText("Acme Corp");
    await userEvent.click(screen.getByRole("button", { name: /Acme Corp/ }));
    await userEvent.click(screen.getByRole("option", { name: /Beta Inc/ }));

    await waitFor(() => expect(screen.getAllByText("Beta Inc").length).toBeGreaterThan(0));
    // O trigger (fechado) agora mostra o novo tenant ativo.
    expect(screen.getByRole("button")).toHaveTextContent("Beta Inc");
  });

  it("selecionar o tenant já ativo não chama switch-tenant novamente", async () => {
    let switchCalls = 0;
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
      ),
      http.post("/api/auth/switch-tenant", () => {
        switchCalls += 1;
        return HttpResponse.json({ token: "msw-token-admin-1", tenant_id: "tenant-1" });
      })
    );
    await loginAs("owner@vane.app");
    renderSwitcher();

    await screen.findByText("Acme Corp");
    await userEvent.click(screen.getByRole("button", { name: /Acme Corp/ }));
    await userEvent.click(screen.getByRole("option", { name: /Acme Corp/ }));

    expect(switchCalls).toBe(0);
  });

  it("membership sem name usa tenant_id como fallback de exibição (Edge Case)", async () => {
    useMultiMembershipMe([
      { tenant_id: "tenant-legacy", role: "owner", name: "", plan_tier: "" },
      { tenant_id: "tenant-2", role: "operator", name: "Beta Inc", plan_tier: "" },
    ]);
    await loginAs("owner@vane.app");
    renderSwitcher();

    await waitFor(() => expect(screen.getByText("tenant-legacy")).toBeInTheDocument());
  });
});
