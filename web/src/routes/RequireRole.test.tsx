import { describe, it, expect, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import { AuthProvider } from "../auth/AuthProvider";
import { RequireAuth, RequireRole } from "./RequireRole";
import { apiFetch } from "../lib/apiClient";

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

function App({ initialEntry = "/protected" }: { initialEntry?: string }) {
  return (
    <MemoryRouter initialEntries={[initialEntry]}>
      <AuthProvider>
        <Routes>
          <Route path="/login" element={<div>login page</div>} />
          <Route path="/" element={<div>home page</div>} />
          <Route
            path="/protected"
            element={
              <RequireAuth>
                <RequireRole roles={["owner"]}>
                  <div>owner only content</div>
                </RequireRole>
              </RequireAuth>
            }
          />
        </Routes>
      </AuthProvider>
    </MemoryRouter>
  );
}

describe("RequireAuth / RequireRole", () => {
  it("não autenticado é redirecionado para /login", async () => {
    render(<App />);
    expect(await screen.findByText("login page")).toBeInTheDocument();
  });

  it("papel errado é redirecionado para / (acesso direto por URL)", async () => {
    await loginAs("viewer@vane.app");
    render(<App />);
    expect(await screen.findByText("home page")).toBeInTheDocument();
  });

  it("papel certo renderiza o conteúdo protegido", async () => {
    await loginAs("owner@vane.app");
    render(<App />);
    expect(await screen.findByText("owner only content")).toBeInTheDocument();
  });
});
