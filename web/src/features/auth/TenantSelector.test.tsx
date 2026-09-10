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
    { tenant_id: "tenant-1", role: "owner" },
    { tenant_id: "tenant-2", role: "operator" },
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
  it("usuário com 1 membership nunca vê a tela de seleção - login vai direto para o dashboard (T17)", async () => {
    renderAppAt("/login");
    await login();

    await waitFor(() => expect(screen.queryByText("Selecione uma organização")).not.toBeInTheDocument());
    // Chegou em alguma rota autenticada normal (redirect da raiz para
    // /domains, RootRoute + RequireAuth) - nunca preso em /select-tenant.
    await waitFor(() => expect(screen.queryByLabelText("E-mail")).not.toBeInTheDocument());
  });

  it("usuário com >1 membership vê a lista de organizações após o login (T17, TENANT-19)", async () => {
    useMultiMembershipMe();
    renderAppAt("/login");
    await login();

    expect(await screen.findByText("Selecione uma organização")).toBeInTheDocument();
    expect(screen.getByText("tenant-1")).toBeInTheDocument();
    expect(screen.getByText("tenant-2")).toBeInTheDocument();
  });

  it("selecionar uma organização chama switch-tenant e leva para o dashboard (T17, TENANT-20)", async () => {
    useMultiMembershipMe();
    renderAppAt("/login");
    await login();

    await screen.findByText("Selecione uma organização");
    await userEvent.click(screen.getByRole("button", { name: /tenant-2/ }));

    await waitFor(() => expect(screen.queryByText("Selecione uma organização")).not.toBeInTheDocument());
  });

  it("visita direta a /select-tenant com 1 membership redireciona para fora da tela (T17)", async () => {
    renderAppAt("/login");
    await login();

    // Já autenticado com 1 membership só - uma navegação direta para
    // /select-tenant não teria nada para listar, então SelectTenantRoute
    // redireciona para "/" em vez de mostrar uma lista vazia.
    await waitFor(() => expect(screen.queryByLabelText("E-mail")).not.toBeInTheDocument());
    expect(screen.queryByText("Selecione uma organização")).not.toBeInTheDocument();
  });

  it("erro do switch-tenant mostra mensagem inline sem navegar (T17, TENANT-21)", async () => {
    useMultiMembershipMe();
    server.use(
      http.post("/api/auth/switch-tenant", () =>
        HttpResponse.json({ error: "no access to that tenant" }, { status: 403 })
      )
    );
    renderAppAt("/login");
    await login();

    await screen.findByText("Selecione uma organização");
    await userEvent.click(screen.getByRole("button", { name: /tenant-1/ }));

    expect(await screen.findByRole("alert")).toHaveTextContent("no access to that tenant");
    expect(screen.getByText("Selecione uma organização")).toBeInTheDocument();
  });
});
