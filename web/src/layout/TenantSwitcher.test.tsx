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
          <TenantSwitcher expanded />
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
  it("shows the tenant identity with no dropdown for a user with a single membership (SHELL-10)", async () => {
    await loginAs("owner@vane.app");
    const { container } = render(
      <TestQueryProvider>
        <MemoryRouter>
          <AuthProvider>
            <AdminEmailProbe />
            <TenantSwitcher expanded />
          </AuthProvider>
        </MemoryRouter>
      </TestQueryProvider>
    );

    await screen.findByTestId("admin-email");
    // Identity (name + plan badge) always renders, matching the handoff -
    // only the dropdown affordance is gated on having >1 membership.
    expect(await screen.findByText("tenant-1")).toBeInTheDocument();
    expect(container.querySelector("[aria-haspopup]")).not.toBeInTheDocument();
  });

  it("lists all memberships with name and plan badge, active tenant marked (SHELL-11)", async () => {
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
    // empty plan_tier shows the "Free" pill (spec.md SHELL-21 / design.md Error Handling).
    expect(betaOption).toHaveTextContent("Free");
  });

  it("selecting another tenant calls switchTenant and updates the active tenant (SHELL-11)", async () => {
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
    // The (closed) trigger now shows the new active tenant.
    expect(screen.getByRole("button")).toHaveTextContent("Beta Inc");
  });

  it("selecting the already-active tenant does not call switch-tenant again", async () => {
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

  it("membership with no name uses tenant_id as the display fallback (Edge Case)", async () => {
    useMultiMembershipMe([
      { tenant_id: "tenant-legacy", role: "owner", name: "", plan_tier: "" },
      { tenant_id: "tenant-2", role: "operator", name: "Beta Inc", plan_tier: "" },
    ]);
    await loginAs("owner@vane.app");
    renderSwitcher();

    await waitFor(() => expect(screen.getByText("tenant-legacy")).toBeInTheDocument());
  });
});
