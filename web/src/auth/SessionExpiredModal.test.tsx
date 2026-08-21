import { describe, it, expect } from "vitest";
import { render, screen, act } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import "../lib/i18n";
import { AuthProvider } from "./AuthProvider";
import { SessionExpiredModal } from "./SessionExpiredModal";
import { triggerUnauthorized } from "../lib/apiClient";

function TestApp() {
  return (
    <MemoryRouter initialEntries={["/"]}>
      <AuthProvider>
        <SessionExpiredModal />
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
});
