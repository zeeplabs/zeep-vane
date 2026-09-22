import { describe, it, expect, afterEach } from "vitest";
import { render, screen, act } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Routes, Route, useLocation } from "react-router-dom";
import "../../lib/i18n";
import i18n from "../../lib/i18n";
import { AuthProvider } from "../../auth/AuthProvider";
import { LoginPage } from "./LoginPage";
import { apiFetch } from "../../lib/apiClient";
import { TestQueryProvider } from "../../test/queryClient";
import {
  setTwoFactorEnabled,
  seedRecoveryCode,
  mswValidTotpCode,
  setDeploymentMode,
} from "../../test/msw/handlers";

afterEach(async () => {
  await act(async () => {
    await i18n.changeLanguage("pt-BR");
  });
  try {
    await apiFetch("/api/auth/logout", { method: "POST" });
  } catch {
    /* ignore */
  }
});

function LocationProbe() {
  const location = useLocation();
  return <div data-testid="location">{location.pathname + location.search}</div>;
}

function App() {
  return (
    <TestQueryProvider>
      <MemoryRouter initialEntries={["/login"]}>
        <AuthProvider>
          <LocationProbe />
          <Routes>
            <Route path="/login" element={<LoginPage />} />
            <Route path="/" element={<div>home page</div>} />
          </Routes>
        </AuthProvider>
      </MemoryRouter>
    </TestQueryProvider>
  );
}

async function reachTwoFactorStep() {
  await userEvent.type(screen.getByLabelText("E-mail"), "owner@vane.app");
  await userEvent.type(screen.getByLabelText("Senha"), "demo1234");
  await userEvent.click(screen.getByRole("button", { name: "Entrar" }));
  await screen.findByLabelText("Código de verificação");
}

describe("LoginPage", () => {
  it("correct login redirects to /", async () => {
    render(<App />);
    await userEvent.type(screen.getByLabelText("E-mail"), "owner@vane.app");
    await userEvent.type(screen.getByLabelText("Senha"), "demo1234");
    await userEvent.click(screen.getByRole("button", { name: "Entrar" }));

    expect(await screen.findByText("home page")).toBeInTheDocument();
  });

  it("failed login shows the exact error without redirecting", async () => {
    render(<App />);
    await userEvent.type(screen.getByLabelText("E-mail"), "owner@vane.app");
    await userEvent.type(screen.getByLabelText("Senha"), "senhaerrada");
    await userEvent.click(screen.getByRole("button", { name: "Entrar" }));

    // The real backend (genericLoginErrorBody, internal/api/auth_handler.go)
    // returns this message in English, without i18n - a known UX gap, out of
    // scope for this integration round (see AD-007 backlog).
    expect(await screen.findByRole("alert")).toHaveTextContent("invalid email or password");
    expect(screen.queryByText("home page")).not.toBeInTheDocument();
  });

  it("there is no error-preview toggle (a Figma-prototype-only feature)", () => {
    render(<App />);
    expect(screen.queryByText(/preview/i)).not.toBeInTheDocument();
  });

  it("password visibility toggle switches the input type", async () => {
    render(<App />);
    const passwordInput = screen.getByLabelText("Senha") as HTMLInputElement;
    expect(passwordInput.type).toBe("password");
    await userEvent.click(screen.getByRole("button", { name: "Mostrar senha" }));
    expect(passwordInput.type).toBe("text");
  });

  // LOGIN2FA-01: active 2FA swaps the form for the verification step, without a session.
  it("login with 2FA shows the verification step without authenticating", async () => {
    setTwoFactorEnabled(true);
    render(<App />);
    await reachTwoFactorStep();

    expect(screen.queryByText("home page")).not.toBeInTheDocument();
    expect(screen.getByTestId("location")).toHaveTextContent("/login");
  });

  // LOGIN2FA-02: a valid code completes the login.
  it("valid code completes the login", async () => {
    setTwoFactorEnabled(true);
    render(<App />);
    await reachTwoFactorStep();

    await userEvent.type(screen.getByLabelText("Código de verificação"), mswValidTotpCode);
    await userEvent.click(screen.getByRole("button", { name: "Verificar" }));

    expect(await screen.findByText("home page")).toBeInTheDocument();
  });

  // LOGIN2FA-03: invalid code shows an inline error and keeps the step.
  it("invalid code shows an inline error and keeps the step", async () => {
    setTwoFactorEnabled(true);
    render(<App />);
    await reachTwoFactorStep();

    await userEvent.type(screen.getByLabelText("Código de verificação"), "000000");
    await userEvent.click(screen.getByRole("button", { name: "Verificar" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Código inválido ou expirado. Tente novamente.",
    );
    expect(screen.queryByText("home page")).not.toBeInTheDocument();
    expect(screen.getByLabelText("Código de verificação")).toBeInTheDocument();
  });

  // LOGIN2FA-05: a valid recovery code also completes the login.
  it("valid recovery code completes the login", async () => {
    seedRecoveryCode("REC-0001");
    setTwoFactorEnabled(true);
    render(<App />);
    await reachTwoFactorStep();

    await userEvent.click(screen.getByRole("button", { name: "Usar um código de recuperação" }));
    await userEvent.type(screen.getByLabelText("Código de recuperação"), "REC-0001");
    await userEvent.click(screen.getByRole("button", { name: "Verificar" }));

    expect(await screen.findByText("home page")).toBeInTheDocument();
  });

  // LOGIN2FA-06: invalid recovery code shows an error and keeps the step.
  it("invalid recovery code shows an error and keeps the step", async () => {
    setTwoFactorEnabled(true);
    render(<App />);
    await reachTwoFactorStep();

    await userEvent.click(screen.getByRole("button", { name: "Usar um código de recuperação" }));
    await userEvent.type(screen.getByLabelText("Código de recuperação"), "REC-XXXX");
    await userEvent.click(screen.getByRole("button", { name: "Verificar" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Código inválido ou expirado. Tente novamente.",
    );
    expect(screen.getByLabelText("Código de recuperação")).toBeInTheDocument();
  });

  // LOGIN2FA-07: switching the method clears the displayed error.
  it("switching the method clears the error", async () => {
    setTwoFactorEnabled(true);
    render(<App />);
    await reachTwoFactorStep();

    await userEvent.type(screen.getByLabelText("Código de verificação"), "000000");
    await userEvent.click(screen.getByRole("button", { name: "Verificar" }));
    await screen.findByRole("alert");

    await userEvent.click(screen.getByRole("button", { name: "Usar um código de recuperação" }));

    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  // LOGIN2FA-08/09: Back returns to the credentials; the challenge token never
  // goes into the URL or into browser storage.
  it("Back returns to the credentials and the token is not persisted", async () => {
    setTwoFactorEnabled(true);
    render(<App />);
    await reachTwoFactorStep();

    await userEvent.click(screen.getByRole("button", { name: "Voltar" }));

    expect(screen.getByLabelText("E-mail")).toBeInTheDocument();
    expect(screen.queryByLabelText("Código de verificação")).not.toBeInTheDocument();
    expect(screen.getByTestId("location")).toHaveTextContent("/login");
    expect(JSON.stringify(window.localStorage)).not.toContain("msw-2fa-challenge");
    expect(JSON.stringify(window.sessionStorage)).not.toContain("msw-2fa-challenge");
  });

  // LOGIN2FA-04: a user without 2FA keeps logging in directly.
  it("login without 2FA does not show the verification step", async () => {
    render(<App />);
    await userEvent.type(screen.getByLabelText("E-mail"), "owner@vane.app");
    await userEvent.type(screen.getByLabelText("Senha"), "demo1234");
    await userEvent.click(screen.getByRole("button", { name: "Entrar" }));

    expect(await screen.findByText("home page")).toBeInTheDocument();
    expect(screen.queryByLabelText("Código de verificação")).not.toBeInTheDocument();
  });

  // LOGIN2FA-12: all new strings render in English with the en locale.
  it("renders the 2FA step in English when the locale is en", async () => {
    setTwoFactorEnabled(true);
    await act(async () => {
      await i18n.changeLanguage("en");
    });
    render(<App />);

    await userEvent.type(screen.getByLabelText("Email"), "owner@vane.app");
    await userEvent.type(screen.getByLabelText("Password"), "demo1234");
    await userEvent.click(screen.getByRole("button", { name: "Sign in" }));

    expect(await screen.findByLabelText("Verification code")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Verify" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Back" })).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Use a recovery code" }));
    expect(screen.getByLabelText("Recovery code")).toBeInTheDocument();
  });

  // DEPMODE-07/08: the "Criar conta" link only appears when the installation
  // accepts signup.
  it("shows the 'Criar conta' link in saas mode (AD-033)", async () => {
    setDeploymentMode("saas");
    render(<App />);

    expect(await screen.findByRole("link", { name: "Criar conta" })).toBeInTheDocument();
  });

  it("hides the 'Criar conta' link in self_hosted mode (AD-033)", async () => {
    setDeploymentMode("self_hosted");
    render(<App />);

    await screen.findByLabelText("E-mail");
    expect(screen.queryByRole("link", { name: "Criar conta" })).not.toBeInTheDocument();
  });
});
