import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import "../../lib/i18n";
import { SignupPage } from "./SignupPage";
import { TestQueryProvider } from "../../test/queryClient";
import { AuthProvider } from "../../auth/AuthProvider";
import { setDeploymentMode } from "../../test/msw/handlers";

function App() {
  return (
    <TestQueryProvider>
      <MemoryRouter initialEntries={["/signup"]}>
        <AuthProvider>
          <Routes>
            <Route path="/signup" element={<SignupPage />} />
            <Route path="/login" element={<div>login page</div>} />
          </Routes>
        </AuthProvider>
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
  it("a valid signup shows the pending-verification screen (T18, TENANT-08)", async () => {
    render(<App />);

    await fillAndSubmit("Acme Inc.", "founder@acme.example.com", "demo1234");

    expect(await screen.findByText("Verifique seu e-mail")).toBeInTheDocument();
    expect(
      screen.getByText((_, node) => node?.textContent === "Enviamos um link de verificação para founder@acme.example.com. Clique nele para ativar sua conta.")
    ).toBeInTheDocument();
    // The signup form should no longer be visible.
    expect(screen.queryByLabelText("E-mail")).not.toBeInTheDocument();
  });

  it("clicking resend calls the resend endpoint and shows confirmation (T18, TENANT-11)", async () => {
    render(<App />);
    await fillAndSubmit("Acme Inc.", "founder@acme.example.com", "demo1234");

    await screen.findByText("Verifique seu e-mail");
    await userEvent.click(screen.getByRole("button", { name: "Reenviar e-mail de verificação" }));

    expect(await screen.findByText("E-mail de verificação reenviado.")).toBeInTheDocument();
  });

  it("a second signup attempt with the same still-unverified email shows a 409 error (spec.md edge case)", async () => {
    const first = render(<App />);
    await fillAndSubmit("Acme Inc.", "founder@acme.example.com", "demo1234");
    await screen.findByText("Verifique seu e-mail");
    first.unmount();

    // Second attempt - simulates another tab/reload filling the form
    // again for the same email before verifying the first one.
    render(<App />);
    await fillAndSubmit("Acme Inc.", "founder@acme.example.com", "demo1234");

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Já existe um cadastro pendente de verificação para este e-mail. Verifique sua caixa de entrada."
    );
  });

  it("a password outside the 8-72 character range shows a validation error (T9's weak-password gate)", async () => {
    render(<App />);
    await fillAndSubmit("Acme Inc.", "founder2@acme.example.com", "short");

    expect(await screen.findByRole("alert")).toHaveTextContent("A senha deve ter entre 8 e 72 caracteres.");
    expect(screen.queryByText("Verifique seu e-mail")).not.toBeInTheDocument();
  });

  it("the 'já tem conta' link leads back to login", async () => {
    render(<App />);
    await userEvent.click(screen.getByText("Já tem conta? Entrar"));
    expect(await screen.findByText("login page")).toBeInTheDocument();
  });

  // DEPMODE-07: in self-hosted mode, shows a restricted-access message instead
  // of the form - reflects the backend's real 404 on these routes.
  it("shows a restricted-access message in self_hosted mode, without displaying the form (AD-033)", async () => {
    setDeploymentMode("self_hosted");
    render(<App />);

    expect(await screen.findByText("Cadastro indisponível")).toBeInTheDocument();
    expect(screen.queryByLabelText("Nome da organização")).not.toBeInTheDocument();
  });
});
