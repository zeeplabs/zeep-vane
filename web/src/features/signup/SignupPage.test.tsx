import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import "../../lib/i18n";
import { SignupPage } from "./SignupPage";
import { TestQueryProvider } from "../../test/queryClient";

function App() {
  return (
    <TestQueryProvider>
      <MemoryRouter initialEntries={["/signup"]}>
        <Routes>
          <Route path="/signup" element={<SignupPage />} />
          <Route path="/login" element={<div>login page</div>} />
        </Routes>
      </MemoryRouter>
    </TestQueryProvider>
  );
}

async function fillAndSubmit(tenantName: string, email: string, password: string) {
  await userEvent.type(screen.getByLabelText("Nome da organização"), tenantName);
  await userEvent.type(screen.getByLabelText("E-mail"), email);
  await userEvent.type(screen.getByLabelText("Senha"), password);
  await userEvent.click(screen.getByRole("button", { name: "Criar conta" }));
}

describe("SignupPage", () => {
  it("signup válido mostra a tela de verificação pendente (T18, TENANT-08)", async () => {
    render(<App />);

    await fillAndSubmit("Acme Inc.", "founder@acme.example.com", "demo1234");

    expect(await screen.findByText("Verifique seu e-mail")).toBeInTheDocument();
    expect(
      screen.getByText((_, node) => node?.textContent === "Enviamos um link de verificação para founder@acme.example.com. Clique nele para ativar sua conta.")
    ).toBeInTheDocument();
    // O formulário de signup não deve mais estar visível.
    expect(screen.queryByLabelText("E-mail")).not.toBeInTheDocument();
  });

  it("clicar em reenviar chama o endpoint de reenvio e mostra confirmação (T18, TENANT-11)", async () => {
    render(<App />);
    await fillAndSubmit("Acme Inc.", "founder@acme.example.com", "demo1234");

    await screen.findByText("Verifique seu e-mail");
    await userEvent.click(screen.getByRole("button", { name: "Reenviar e-mail de verificação" }));

    expect(await screen.findByText("E-mail de verificação reenviado.")).toBeInTheDocument();
  });

  it("segunda tentativa de signup com o mesmo e-mail ainda não verificado mostra erro 409 (spec.md edge case)", async () => {
    const first = render(<App />);
    await fillAndSubmit("Acme Inc.", "founder@acme.example.com", "demo1234");
    await screen.findByText("Verifique seu e-mail");
    first.unmount();

    // Segunda tentativa - simula outra aba/reload preenchendo o formulário
    // de novo para o mesmo e-mail antes de verificar o primeiro.
    render(<App />);
    await fillAndSubmit("Acme Inc.", "founder@acme.example.com", "demo1234");

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Já existe um cadastro pendente de verificação para este e-mail. Verifique sua caixa de entrada."
    );
  });

  it("senha fora do intervalo de 8-72 caracteres mostra erro de validação (T9's weak-password gate)", async () => {
    render(<App />);
    await fillAndSubmit("Acme Inc.", "founder2@acme.example.com", "short");

    expect(await screen.findByRole("alert")).toHaveTextContent("A senha deve ter entre 8 e 72 caracteres.");
    expect(screen.queryByText("Verifique seu e-mail")).not.toBeInTheDocument();
  });

  it("link 'já tem conta' leva de volta ao login", async () => {
    render(<App />);
    await userEvent.click(screen.getByText("Já tem conta? Entrar"));
    expect(await screen.findByText("login page")).toBeInTheDocument();
  });
});
