import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import { http, HttpResponse } from "msw";
import "../../lib/i18n";
import { VerifyEmailPage } from "./VerifyEmailPage";
import { SignupPage } from "./SignupPage";
import { server } from "../../test/msw/server";
import { TestQueryProvider } from "../../test/queryClient";
import { AuthProvider } from "../../auth/AuthProvider";

function App(initialPath: string) {
  return (
    <TestQueryProvider>
      <MemoryRouter initialEntries={[initialPath]}>
        <AuthProvider>
          <Routes>
            <Route path="/signup" element={<SignupPage />} />
            <Route path="/verify-email/:token" element={<VerifyEmailPage />} />
            <Route path="/login" element={<div>login page</div>} />
          </Routes>
        </AuthProvider>
      </MemoryRouter>
    </TestQueryProvider>
  );
}

describe("VerifyEmailPage", () => {
  it("a valid token for a pending signup marks the email as verified (T18, TENANT-09/10)", async () => {
    // Create the pending signup first, so the MSW fixture's deterministic
    // token (signupVerifyToken) actually exists.
    render(App("/signup"));
    await userEvent.type(screen.getByLabelText("Nome da organização"), "Acme Inc.");
    await userEvent.type(screen.getByLabelText("E-mail"), "verify-me@acme.example.com");
    await userEvent.type(screen.getByLabelText("Senha"), "demo1234");
    await userEvent.click(screen.getByRole("button", { name: "Criar conta" }));
    await screen.findByText("Verifique seu e-mail");

    render(App("/verify-email/verify-token-for-verify-me@acme.example.com"));

    expect(await screen.findByText("E-mail verificado")).toBeInTheDocument();
    expect(screen.getByText("Sua conta foi ativada. Você já pode entrar.")).toBeInTheDocument();
  });

  it("an invalid/expired token shows a clear error, without verifying anything (T10)", async () => {
    render(App("/verify-email/no-such-token"));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Este link de verificação é inválido ou expirou. Solicite um novo reenvio."
    );
  });

  it("shows the loading state before the backend responds", async () => {
    server.use(
      http.get("/api/signup/verify/:token", async () => {
        await new Promise((resolve) => setTimeout(resolve, 20));
        return HttpResponse.json({ status: "verified" });
      })
    );
    render(App("/verify-email/some-token"));

    expect(screen.getByText("Verificando seu e-mail...")).toBeInTheDocument();
    expect(await screen.findByText("E-mail verificado")).toBeInTheDocument();
  });

  it("the 'ir para o login' link leads to /login after verification", async () => {
    render(App("/signup"));
    await userEvent.type(screen.getByLabelText("Nome da organização"), "Acme Inc.");
    await userEvent.type(screen.getByLabelText("E-mail"), "verify-nav@acme.example.com");
    await userEvent.type(screen.getByLabelText("Senha"), "demo1234");
    await userEvent.click(screen.getByRole("button", { name: "Criar conta" }));
    await screen.findByText("Verifique seu e-mail");

    render(App("/verify-email/verify-token-for-verify-nav@acme.example.com"));
    await screen.findByText("E-mail verificado");

    await userEvent.click(screen.getByText("Ir para o login"));
    expect(await screen.findByText("login page")).toBeInTheDocument();
  });
});
