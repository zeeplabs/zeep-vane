import { describe, it, expect } from "vitest";
import { render, screen, waitFor, act } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import App from "./App";
import { setBootstrapped } from "./test/msw/handlers";
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
