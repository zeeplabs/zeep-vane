import { describe, it, expect } from "vitest";
import { render, screen, act, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import "../lib/i18n";
import { AuthProvider, useAuth } from "./AuthProvider";
import { SessionExpiredModal } from "./SessionExpiredModal";
import { triggerUnauthorized } from "../lib/apiClient";

function StatusProbe() {
  const { status } = useAuth();
  return <span data-testid="auth-status">{status}</span>;
}

function TestApp() {
  return (
    <MemoryRouter initialEntries={["/"]}>
      <AuthProvider>
        <SessionExpiredModal />
        <StatusProbe />
        <Routes>
          <Route path="/" element={<div>home</div>} />
          <Route path="/login" element={<div>login page</div>} />
        </Routes>
      </AuthProvider>
    </MemoryRouter>
  );
}

describe("SessionExpiredModal", () => {
  it("disparar 401 simulado abre o modal", async () => {
    render(<TestApp />);
    await act(async () => {
      triggerUnauthorized();
    });
    expect(await screen.findByText("Sessão expirada")).toBeInTheDocument();
  });

  it("clique no backdrop não fecha o modal", async () => {
    render(<TestApp />);
    await act(async () => {
      triggerUnauthorized();
    });
    await screen.findByText("Sessão expirada");

    const overlay = document.querySelector("[data-radix-dialog-overlay]") as HTMLElement | null;
    if (overlay) {
      await userEvent.click(overlay);
    }
    expect(screen.getByText("Sessão expirada")).toBeInTheDocument();
  });

  it("clique no CTA navega para /login", async () => {
    render(<TestApp />);
    await act(async () => {
      triggerUnauthorized();
    });
    await screen.findByText("Sessão expirada");

    await userEvent.click(screen.getByRole("button", { name: "Ir para o login" }));
    expect(await screen.findByText("login page")).toBeInTheDocument();
  });

  // Regressão: o probe de boot em GET /api/auth/me sempre 401 pra um
  // visitante anônimo (inclusive na própria /login) - isso NUNCA é uma
  // sessão que expirou, é a ausência de sessão alguma. AuthProvider passa
  // skipUnauthorizedHandler nesse fetch especificamente por isto.
  it("boot anônimo (401 esperado em /api/auth/me) não abre o modal de sessão expirada", async () => {
    render(<TestApp />);
    await waitFor(() => expect(screen.getByTestId("auth-status")).toHaveTextContent("anonymous"));
    expect(screen.queryByText("Sessão expirada")).not.toBeInTheDocument();
  });
});
