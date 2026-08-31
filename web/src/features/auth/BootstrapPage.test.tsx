import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import "../../lib/i18n";
import { BootstrapPage } from "./BootstrapPage";
import { setBootstrapped } from "../../test/msw/handlers";
import { TestQueryProvider } from "../../test/queryClient";

function App() {
  return (
    <TestQueryProvider>
      <MemoryRouter initialEntries={["/bootstrap"]}>
        <Routes>
          <Route path="/bootstrap" element={<BootstrapPage />} />
          <Route path="/login" element={<div>login page</div>} />
        </Routes>
      </MemoryRouter>
    </TestQueryProvider>
  );
}

async function fillForm(email: string, password: string, confirmPassword: string) {
  await userEvent.type(screen.getByLabelText("Nome"), "Ana Owner");
  await userEvent.type(screen.getByLabelText("E-mail"), email);
  await userEvent.type(screen.getByLabelText("Senha"), password);
  await userEvent.type(screen.getByLabelText("Confirmar senha"), confirmPassword);
}

describe("BootstrapPage", () => {
  let assignSpy: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    assignSpy = vi.fn();
    Object.defineProperty(window, "location", {
      value: { ...window.location, assign: assignSpy },
      writable: true,
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("submissão válida com senhas iguais cria o admin e navega para a raiz (SHD-16/SHD-18)", async () => {
    setBootstrapped(false);
    render(<App />);

    await fillForm("owner@vane.app", "demo1234", "demo1234");
    await userEvent.click(screen.getByRole("button", { name: "Criar administrador" }));

    await waitFor(() => expect(assignSpy).toHaveBeenCalledWith("/"));
  });

  it("409 (já bootstrapado) mostra erro inline e link para /login, sem navegar (SHD-15)", async () => {
    setBootstrapped(true);
    render(<App />);

    await fillForm("owner@vane.app", "demo1234", "demo1234");
    await userEvent.click(screen.getByRole("button", { name: "Criar administrador" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Esta instância já tem um administrador."
    );
    expect(screen.getByRole("link", { name: "Ir para o login" })).toHaveAttribute("href", "/login");
    expect(assignSpy).not.toHaveBeenCalled();
  });

  it("senha e confirmação diferentes mostram erro de validação sem chamar a API", async () => {
    setBootstrapped(false);
    const fetchSpy = vi.spyOn(global, "fetch");
    render(<App />);

    await fillForm("owner@vane.app", "demo1234", "outrasenha");
    await userEvent.click(screen.getByRole("button", { name: "Criar administrador" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("As senhas não coincidem.");
    expect(assignSpy).not.toHaveBeenCalled();
    // /api/instance/branding is fetched unconditionally by useBrandLogoUrl
    // on mount - only /api/bootstrap itself must never be called.
    expect(fetchSpy).not.toHaveBeenCalledWith("/api/bootstrap", expect.anything());
  });

  it("campos nome/email/senha/confirmação são obrigatórios, seguindo a convenção do LoginPage", () => {
    render(<App />);

    expect(screen.getByLabelText("Nome")).toBeRequired();
    expect(screen.getByLabelText("E-mail")).toBeRequired();
    expect(screen.getByLabelText("Senha")).toBeRequired();
    expect(screen.getByLabelText("Confirmar senha")).toBeRequired();
  });
});
