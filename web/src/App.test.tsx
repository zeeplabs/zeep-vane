import { describe, it, expect } from "vitest";
import { render, screen, waitFor, act } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { http, HttpResponse } from "msw";
import App from "./App";
import { setBootstrapped } from "./test/msw/handlers";
import { server } from "./test/msw/server";
import { TestQueryProvider } from "./test/queryClient";
import { apiFetch } from "./lib/apiClient";

function renderAppAt(path: string) {
  return render(
    <TestQueryProvider>
      <MemoryRouter initialEntries={[path]}>
        <App />
      </MemoryRouter>
    </TestQueryProvider>
  );
}

// All scenarios below are anonymous visitor (no session) - the
// bootstrap guard (SHD-19, SHD-21) only decides between /bootstrap and /login,
// never interacts with RequireAuth/RequireRole.
describe("App - bootstrap redirect guard", () => {
  it("loading /login with needsBootstrap=true redirects to /bootstrap (SHD-19)", async () => {
    setBootstrapped(false);
    renderAppAt("/login");

    await waitFor(() =>
      expect(screen.getByText("Crie a conta do primeiro administrador")).toBeInTheDocument()
    );
  });

  it("loading / with needsBootstrap=true redirects to /bootstrap (SHD-19)", async () => {
    setBootstrapped(false);
    renderAppAt("/");

    await waitFor(() =>
      expect(screen.getByText("Crie a conta do primeiro administrador")).toBeInTheDocument()
    );
  });

  it("loading /bootstrap with needsBootstrap=false redirects to /login (SHD-21)", async () => {
    setBootstrapped(true);
    renderAppAt("/bootstrap");

    await waitFor(() => expect(screen.getByRole("heading", { name: "Entrar" })).toBeInTheDocument());
    expect(screen.queryByText("Crie a conta do primeiro administrador")).not.toBeInTheDocument();
  });

  it("loading /bootstrap with needsBootstrap=true renders BootstrapPage without a redirect loop (SHD-21)", async () => {
    setBootstrapped(false);
    renderAppAt("/bootstrap");

    await waitFor(() =>
      expect(screen.getByText("Crie a conta do primeiro administrador")).toBeInTheDocument()
    );
    // Gives time for any possible second round of effects and confirms
    // that the screen doesn't jump back to /login - that would prove a loop.
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 50));
    });
    expect(screen.getByText("Crie a conta do primeiro administrador")).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Entrar" })).not.toBeInTheDocument();
  });
});

// AD-018: "/" is shared by two audiences on the real embedded SPA - an
// operator on the admin domain, and a visitor on a published status
// page's own custom domain (Host-routed to the same binary, same static
// bundle). RootRoute tells them apart by probing GET /api/public-status,
// which only the public HTTPS listener ever wires up in production.
describe("App - RootRoute (AD-018 status-page-domain vs admin-domain)", () => {
  it("/api/public-status 200 renders the public status page, without requiring a session or redirecting to /login", async () => {
    server.use(
      http.get("/api/public-status", () => {
        return HttpResponse.json({
          company: { name: "Acme Public Domain Co", logo_url: null },
          services: [],
          incidents: { active: [], resolved: { items: [], total: 0, page: 1, page_size: 10 } },
        });
      }),
    );
    renderAppAt("/");

    await waitFor(() => expect(screen.getByText("Acme Public Domain Co")).toBeInTheDocument());
    expect(screen.queryByRole("heading", { name: "Entrar" })).not.toBeInTheDocument();
  });

  it("/api/public-status 404 (admin domain) follows the normal bootstrap/login flow", async () => {
    setBootstrapped(true);
    renderAppAt("/");

    await waitFor(() => expect(screen.getByRole("heading", { name: "Entrar" })).toBeInTheDocument());
  });
});

// PROFPAGE-01/03: /profile lives inside the authenticated group (no RequireRole)
// and is accessible to any role; without a session, RequireAuth redirects.
describe("App - /profile route", () => {
  it("authenticated renders the ProfilePage", async () => {
    setBootstrapped(true);
    await apiFetch("/api/auth/login", {
      method: "POST",
      body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
    });
    renderAppAt("/profile");

    await waitFor(() =>
      expect(screen.getByRole("heading", { level: 1, name: "Meu Perfil" })).toBeInTheDocument()
    );
    expect(screen.getByText("Sessões ativas")).toBeInTheDocument();
  });

  it("without a session redirects to /login", async () => {
    setBootstrapped(true);
    renderAppAt("/profile");

    await waitFor(() => expect(screen.getByRole("heading", { name: "Entrar" })).toBeInTheDocument());
    expect(screen.queryByRole("heading", { level: 1, name: "Meu Perfil" })).not.toBeInTheDocument();
  });
});

// OVW-01: "/" lands on the Overview page (not /domains) for an authenticated
// user with a resolved tenant; /overview is also directly addressable.
describe("App - Overview route", () => {
  it("authenticated loading / renders the OverviewPage (OVW-01)", async () => {
    setBootstrapped(true);
    await apiFetch("/api/auth/login", {
      method: "POST",
      body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
    });
    renderAppAt("/");

    await waitFor(() =>
      expect(screen.getByRole("heading", { level: 1, name: "Visão geral" })).toBeInTheDocument()
    );
  });

  it("direct visit to /overview renders the OverviewPage", async () => {
    setBootstrapped(true);
    await apiFetch("/api/auth/login", {
      method: "POST",
      body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
    });
    renderAppAt("/overview");

    await waitFor(() =>
      expect(screen.getByRole("heading", { level: 1, name: "Visão geral" })).toBeInTheDocument()
    );
  });
});
