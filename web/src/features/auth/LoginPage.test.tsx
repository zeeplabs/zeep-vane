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
} from "../../test/msw/handlers";

afterEach(async () => {
  await act(async () => {
    await i18n.changeLanguage("pt");
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

  // LOGIN2FA-01: 2FA ativa troca o form pelo passo de verificação, sem sessão.
  it("login com 2FA mostra o passo de verificação sem autenticar", async () => {
    setTwoFactorEnabled(true);
    render(<App />);
    await reachTwoFactorStep();

    expect(screen.queryByText("home page")).not.toBeInTheDocument();
    expect(screen.getByTestId("location")).toHaveTextContent("/login");
  });

  // LOGIN2FA-02: código válido conclui o login.
  it("código válido conclui o login", async () => {
    setTwoFactorEnabled(true);
    render(<App />);
    await reachTwoFactorStep();

    await userEvent.type(screen.getByLabelText("Código de verificação"), mswValidTotpCode);
    await userEvent.click(screen.getByRole("button", { name: "Verificar" }));

    expect(await screen.findByText("home page")).toBeInTheDocument();
  });

  // LOGIN2FA-03: código inválido mostra erro inline e mantém o passo.
  it("código inválido mostra erro inline e mantém o passo", async () => {
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

  // LOGIN2FA-05: o código de recuperação válido também conclui o login.
  it("código de recuperação válido conclui o login", async () => {
    seedRecoveryCode("REC-0001");
    setTwoFactorEnabled(true);
    render(<App />);
    await reachTwoFactorStep();

    await userEvent.click(screen.getByRole("button", { name: "Usar um código de recuperação" }));
    await userEvent.type(screen.getByLabelText("Código de recuperação"), "REC-0001");
    await userEvent.click(screen.getByRole("button", { name: "Verificar" }));

    expect(await screen.findByText("home page")).toBeInTheDocument();
  });

  // LOGIN2FA-06: código de recuperação inválido mostra erro e mantém o passo.
  it("código de recuperação inválido mostra erro e mantém o passo", async () => {
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

  // LOGIN2FA-07: alternar o método limpa o erro exibido.
  it("alternar o método limpa o erro", async () => {
    setTwoFactorEnabled(true);
    render(<App />);
    await reachTwoFactorStep();

    await userEvent.type(screen.getByLabelText("Código de verificação"), "000000");
    await userEvent.click(screen.getByRole("button", { name: "Verificar" }));
    await screen.findByRole("alert");

    await userEvent.click(screen.getByRole("button", { name: "Usar um código de recuperação" }));

    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  // LOGIN2FA-08/09: Voltar retorna às credenciais; o challenge token nunca vai
  // para a URL nem para o armazenamento do navegador.
  it("Voltar retorna às credenciais e o token não é persistido", async () => {
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

  // LOGIN2FA-04: usuário sem 2FA continua entrando direto.
  it("login sem 2FA não mostra o passo de verificação", async () => {
    render(<App />);
    await userEvent.type(screen.getByLabelText("E-mail"), "owner@vane.app");
    await userEvent.type(screen.getByLabelText("Senha"), "demo1234");
    await userEvent.click(screen.getByRole("button", { name: "Entrar" }));

    expect(await screen.findByText("home page")).toBeInTheDocument();
    expect(screen.queryByLabelText("Código de verificação")).not.toBeInTheDocument();
  });

  // LOGIN2FA-12: todas as strings novas renderizam em inglês com locale en.
  it("renderiza o passo de 2FA em inglês quando o locale é en", async () => {
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
});
