import { describe, it, expect, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { http, HttpResponse } from "msw";
import App from "../../App";
import { apiFetch } from "../../lib/apiClient";
import { server } from "../../test/msw/server";
import { TestQueryProvider } from "../../test/queryClient";

// Multi-membership fixture used by the ">1 membership" scenarios below -
// mirrors the real backend's meResponse shape (multi-tenancy-core, T7):
// tenant_id + role per entry, active_tenant_id empty/omitted until a
// selection is made (T17, TENANT-19/20/21).
const multiMembershipMe = {
  id: "admin-1",
  email: "owner@vane.app",
  name: "Ana Owner",
  role: "owner",
  memberships: [
    { tenant_id: "tenant-1", role: "owner", name: "Acme Corp", plan_tier: "scale" },
    { tenant_id: "tenant-2", role: "operator", name: "Beta Inc", plan_tier: "" },
  ],
};

// Legacy/edge-case fixture (new-layout-migration, SHELL-20 Edge Case): a
// membership row with no `name` set - the selector must fall back to
// `tenant_id` instead of rendering a blank row.
const legacyMembershipMe = {
  id: "admin-1",
  email: "owner@vane.app",
  name: "Ana Owner",
  role: "owner",
  memberships: [
    { tenant_id: "tenant-1", role: "owner", name: "Acme Corp", plan_tier: "scale" },
    { tenant_id: "tenant-legacy", role: "operator", name: "", plan_tier: "" },
  ],
};

// activeTenantId tracks this test's own session state across the two
// calls a real switch makes (POST /api/auth/switch-tenant, then a
// re-fetch of GET /api/auth/me) - "" (never selected) until a successful
// switch sets it, mirroring the real backend's active_tenant_id semantics
// (T7/T8).
function useMultiMembershipMe() {
  let activeTenantId = "";
  server.use(
    http.get("/api/auth/me", () =>
      HttpResponse.json({ ...multiMembershipMe, active_tenant_id: activeTenantId || undefined })
    ),
    http.post("/api/auth/switch-tenant", async ({ request }) => {
      const body = (await request.json()) as { tenant_id?: string };
      if (body.tenant_id !== "tenant-1" && body.tenant_id !== "tenant-2") {
        return HttpResponse.json({ error: "no access to that tenant" }, { status: 403 });
      }
      activeTenantId = body.tenant_id;
      return HttpResponse.json({ token: "msw-token-admin-1", tenant_id: body.tenant_id });
    })
  );
}

function renderAppAt(path: string) {
  return render(
    <TestQueryProvider>
      <MemoryRouter initialEntries={[path]}>
        <App />
      </MemoryRouter>
    </TestQueryProvider>
  );
}

async function login() {
  await userEvent.type(await screen.findByLabelText("E-mail"), "owner@vane.app");
  await userEvent.type(screen.getByLabelText("Senha"), "demo1234");
  await userEvent.click(screen.getByRole("button", { name: "Entrar" }));
}

afterEach(async () => {
  try {
    await apiFetch("/api/auth/logout", { method: "POST" });
  } catch {
    /* ignore */
  }
});

describe("TenantSelector", () => {
  it("user with 1 membership never sees the selection screen - login goes straight to the dashboard (T17)", async () => {
    renderAppAt("/login");
    await login();

    await waitFor(() => expect(screen.queryByText("Selecione uma organização")).not.toBeInTheDocument());
    // Landed on some normal authenticated route (redirect from the root to
    // /domains, RootRoute + RequireAuth) - never stuck on /select-tenant.
    await waitFor(() => expect(screen.queryByLabelText("E-mail")).not.toBeInTheDocument());
  });

  it("user with >1 membership sees the organization list after login (T17, TENANT-19)", async () => {
    useMultiMembershipMe();
    renderAppAt("/login");
    await login();

    expect(await screen.findByText("Selecione uma organização")).toBeInTheDocument();
    expect(screen.getByText("Acme Corp")).toBeInTheDocument();
    expect(screen.getByText("Beta Inc")).toBeInTheDocument();
  });

  it("membership without a name uses tenant_id as the display fallback (new-layout-migration, SHELL-20 Edge Case)", async () => {
    server.use(
      http.get("/api/auth/me", () =>
        HttpResponse.json({ ...legacyMembershipMe, active_tenant_id: undefined })
      )
    );
    renderAppAt("/login");
    await login();

    await screen.findByText("Selecione uma organização");
    expect(screen.getByText("Acme Corp")).toBeInTheDocument();
    expect(screen.getByText("tenant-legacy")).toBeInTheDocument();
  });

  it("selecting an organization calls switch-tenant and leads to the dashboard (T17, TENANT-20)", async () => {
    useMultiMembershipMe();
    renderAppAt("/login");
    await login();

    await screen.findByText("Selecione uma organização");
    await userEvent.click(screen.getByRole("button", { name: /Beta Inc/ }));

    await waitFor(() => expect(screen.queryByText("Selecione uma organização")).not.toBeInTheDocument());
  });

  it("direct visit to /select-tenant with 1 membership redirects away from the screen (T17)", async () => {
    renderAppAt("/login");
    await login();

    // Already authenticated with only 1 membership - a direct navigation to
    // /select-tenant would have nothing to list, so SelectTenantRoute
    // redirects to "/" instead of showing an empty list.
    await waitFor(() => expect(screen.queryByLabelText("E-mail")).not.toBeInTheDocument());
    expect(screen.queryByText("Selecione uma organização")).not.toBeInTheDocument();
  });

  it("switch-tenant error shows an inline message without navigating (T17, TENANT-21)", async () => {
    useMultiMembershipMe();
    server.use(
      http.post("/api/auth/switch-tenant", () =>
        HttpResponse.json({ error: "no access to that tenant" }, { status: 403 })
      )
    );
    renderAppAt("/login");
    await login();

    await screen.findByText("Selecione uma organização");
    await userEvent.click(screen.getByRole("button", { name: /Acme Corp/ }));

    expect(await screen.findByRole("alert")).toHaveTextContent("no access to that tenant");
    expect(screen.getByText("Selecione uma organização")).toBeInTheDocument();
  });
});
