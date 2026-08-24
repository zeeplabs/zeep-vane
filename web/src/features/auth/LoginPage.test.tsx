import { describe, it, expect, afterEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import "../../lib/i18n";
import { AuthProvider } from "../../auth/AuthProvider";
import { LoginPage } from "./LoginPage";
import { apiFetch } from "../../lib/apiClient";
import { TestQueryProvider } from "../../test/queryClient";

afterEach(async () => {
  try {
    await apiFetch("/api/auth/logout", { method: "POST" });
  } catch {
    /* ignore */
  }
});

function App() {
  return (
    <TestQueryProvider>
      <MemoryRouter initialEntries={["/login"]}>
        <AuthProvider>
          <Routes>
            <Route path="/login" element={<LoginPage />} />
            <Route path="/" element={<div>home page</div>} />
          </Routes>
        </AuthProvider>
      </MemoryRouter>
    </TestQueryProvider>
  );
}

describe("LoginPage", () => {
  it("login correto redireciona para /", async () => {
    render(<App />);
    await userEvent.type(screen.getByLabelText("E-mail"), "owner@vane.app");
    await userEvent.type(screen.getByLabelText("Senha"), "demo1234");
    await userEvent.click(screen.getByRole("button", { name: "Entrar" }));

    expect(await screen.findByText("home page")).toBeInTheDocument();
  });

  it("login falho mostra erro exato sem redirecionar", async () => {
    render(<App />);
    await userEvent.type(screen.getByLabelText("E-mail"), "owner@vane.app");
    await userEvent.type(screen.getByLabelText("Senha"), "senhaerrada");
    await userEvent.click(screen.getByRole("button", { name: "Entrar" }));

    // Backend real (genericLoginErrorBody, internal/api/auth_handler.go) retorna
    // essa mensagem em inglês, sem i18n - gap de UX conhecido, fora do escopo
    // desta rodada de integração (ver AD-007 backlog).
    expect(await screen.findByRole("alert")).toHaveTextContent("invalid email or password");
    expect(screen.queryByText("home page")).not.toBeInTheDocument();
  });

  it("não existe toggle de preview de erro (recurso só do protótipo Figma)", () => {
    render(<App />);
    expect(screen.queryByText(/preview/i)).not.toBeInTheDocument();
  });

  it("toggle de visibilidade da senha alterna o tipo do input", async () => {
    render(<App />);
    const passwordInput = screen.getByLabelText("Senha") as HTMLInputElement;
    expect(passwordInput.type).toBe("password");
    await userEvent.click(screen.getByRole("button", { name: "Mostrar senha" }));
    expect(passwordInput.type).toBe("text");
  });
});
