import { describe, it, expect } from "vitest";
import { render, screen, waitFor, act } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { http, HttpResponse } from "msw";
import App from "./App";
import { setBootstrapped } from "./test/msw/handlers";
import { server } from "./test/msw/server";
import { TestQueryProvider } from "./test/queryClient";

function renderAppAt(path: string) {
  return render(
    <TestQueryProvider>
      <MemoryRouter initialEntries={[path]}>
        <App />
      </MemoryRouter>
    </TestQueryProvider>
  );
}

// Todos os cenários abaixo são de visitante anônimo (nenhuma sessão) - o
// guard de bootstrap (SHD-19, SHD-21) só decide entre /bootstrap e /login,
// nunca interage com RequireAuth/RequireRole.
describe("App - bootstrap redirect guard", () => {
  it("carregar /login com needsBootstrap=true redireciona para /bootstrap (SHD-19)", async () => {
    setBootstrapped(false);
    renderAppAt("/login");

    await waitFor(() =>
      expect(screen.getByText("Crie a conta do primeiro administrador")).toBeInTheDocument()
    );
  });

  it("carregar / com needsBootstrap=true redireciona para /bootstrap (SHD-19)", async () => {
    setBootstrapped(false);
    renderAppAt("/");

    await waitFor(() =>
      expect(screen.getByText("Crie a conta do primeiro administrador")).toBeInTheDocument()
    );
  });

  it("carregar /bootstrap com needsBootstrap=false redireciona para /login (SHD-21)", async () => {
    setBootstrapped(true);
    renderAppAt("/bootstrap");

    await waitFor(() => expect(screen.getByRole("heading", { name: "Entrar" })).toBeInTheDocument());
    expect(screen.queryByText("Crie a conta do primeiro administrador")).not.toBeInTheDocument();
  });

  it("carregar /bootstrap com needsBootstrap=true renderiza BootstrapPage sem loop de redirecionamento (SHD-21)", async () => {
    setBootstrapped(false);
    renderAppAt("/bootstrap");

    await waitFor(() =>
      expect(screen.getByText("Crie a conta do primeiro administrador")).toBeInTheDocument()
    );
    // Dá tempo para qualquer possível segunda rodada de efeitos e confirma
    // que a tela não volta a pular para /login - provaria um loop.
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
  it("/api/public-status 200 renderiza a status page pública, sem exigir sessão nem redirecionar para /login", async () => {
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

  it("/api/public-status 404 (domínio admin) segue o fluxo normal de bootstrap/login", async () => {
    setBootstrapped(true);
    renderAppAt("/");

    await waitFor(() => expect(screen.getByRole("heading", { name: "Entrar" })).toBeInTheDocument());
  });
});
