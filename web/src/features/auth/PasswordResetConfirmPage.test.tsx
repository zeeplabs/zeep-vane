import { describe, it, expect, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import { HttpResponse, http } from "msw";
import "../../lib/i18n";
import { PasswordResetConfirmPage } from "./PasswordResetConfirmPage";
import { seedPasswordResetToken } from "../../test/msw/handlers";
import { server } from "../../test/msw/server";
import { TestQueryProvider } from "../../test/queryClient";

function App(token: string) {
  return (
    <TestQueryProvider>
      <MemoryRouter initialEntries={[`/reset-password/${token}`]}>
        <Routes>
          <Route path="/reset-password/:token" element={<PasswordResetConfirmPage />} />
          <Route path="/login" element={<div>login page</div>} />
        </Routes>
      </MemoryRouter>
    </TestQueryProvider>
  );
}

async function fillForm(password: string, confirmPassword: string) {
  await userEvent.type(screen.getByLabelText("Nova senha"), password);
  await userEvent.type(screen.getByLabelText("Confirmar nova senha"), confirmPassword);
}

describe("PasswordResetConfirmPage", () => {
  it("submissão válida com senhas iguais mostra confirmação de sucesso", async () => {
    seedPasswordResetToken("valid-token", "owner@vane.app");
    render(App("valid-token"));

    await fillForm("demo1234", "demo1234");
    await userEvent.click(screen.getByRole("button", { name: "Redefinir senha" }));

    expect(await screen.findByText("Senha redefinida com sucesso.")).toBeInTheDocument();
  });

  it("botão 'Ir para o login' após sucesso navega para /login", async () => {
    seedPasswordResetToken("valid-token", "owner@vane.app");
    render(App("valid-token"));

    await fillForm("demo1234", "demo1234");
    await userEvent.click(screen.getByRole("button", { name: "Redefinir senha" }));
    await screen.findByText("Senha redefinida com sucesso.");

    await userEvent.click(screen.getByRole("button", { name: "Ir para o login" }));
    expect(await screen.findByText("login page")).toBeInTheDocument();
  });

  it("senha e confirmação diferentes mostram erro de validação sem chamar a API", async () => {
    seedPasswordResetToken("valid-token", "owner@vane.app");
    const fetchSpy = vi.spyOn(global, "fetch");
    render(App("valid-token"));

    await fillForm("demo1234", "outrasenha");
    await userEvent.click(screen.getByRole("button", { name: "Redefinir senha" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("As senhas não coincidem.");
    expect(fetchSpy).not.toHaveBeenCalledWith(
      expect.stringContaining("/api/auth/password-reset/confirm"),
      expect.anything(),
    );
  });

  it("token desconhecido/expirado (401) mostra mensagem genérica sem travar o formulário", async () => {
    render(App("does-not-exist"));

    await fillForm("demo1234", "demo1234");
    await userEvent.click(screen.getByRole("button", { name: "Redefinir senha" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Este link de recuperação é inválido ou expirou. Solicite um novo.",
    );
    expect(screen.getByRole("button", { name: "Redefinir senha" })).not.toBeDisabled();
  });

  it("senha fraca (422) mostra mensagem traduzida, não o texto cru do servidor", async () => {
    seedPasswordResetToken("valid-token", "owner@vane.app");
    render(App("valid-token"));

    await fillForm("short", "short");
    await userEvent.click(screen.getByRole("button", { name: "Redefinir senha" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "A senha deve ter entre 8 e 72 caracteres.",
    );
  });

  it("falha de rede mostra mensagem genérica de fallback", async () => {
    server.use(http.post("/api/auth/password-reset/confirm", () => HttpResponse.error()));
    render(App("valid-token"));

    await fillForm("demo1234", "demo1234");
    await userEvent.click(screen.getByRole("button", { name: "Redefinir senha" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Não foi possível redefinir a senha. Tente novamente.",
    );
  });

  it("botão de envio fica desabilitado durante a submissão, evitando duplo clique", async () => {
    let resolveResponse!: () => void;
    const responseGate = new Promise<void>((resolve) => {
      resolveResponse = resolve;
    });
    server.use(
      http.post("/api/auth/password-reset/confirm", async () => {
        await responseGate;
        return HttpResponse.json({ status: "ok" });
      }),
    );
    seedPasswordResetToken("valid-token", "owner@vane.app");
    render(App("valid-token"));

    await fillForm("demo1234", "demo1234");
    const submitButton = screen.getByRole("button", { name: "Redefinir senha" });
    await userEvent.click(submitButton);

    await waitFor(() => expect(submitButton).toBeDisabled());

    resolveResponse();
    await waitFor(() => expect(screen.getByText("Senha redefinida com sucesso.")).toBeInTheDocument());
  });

  it("nunca envia o token bruto no corpo além do próprio campo esperado pelo backend", async () => {
    const rawToken = "raw-reset-token-must-not-leak";
    seedPasswordResetToken(rawToken, "owner@vane.app");
    let requestBody: unknown;
    server.use(
      http.post("/api/auth/password-reset/confirm", async ({ request }) => {
        requestBody = await request.json();
        return HttpResponse.json({ status: "ok" });
      }),
    );
    render(App(rawToken));

    await fillForm("demo1234", "demo1234");
    await userEvent.click(screen.getByRole("button", { name: "Redefinir senha" }));

    await screen.findByText("Senha redefinida com sucesso.");
    expect(requestBody).toEqual({ token: rawToken, new_password: "demo1234" });
    expect(document.body.textContent).not.toContain(rawToken);
  });

  it("campos de senha e confirmação são obrigatórios", () => {
    render(App("valid-token"));

    expect(screen.getByLabelText("Nova senha")).toBeRequired();
    expect(screen.getByLabelText("Confirmar nova senha")).toBeRequired();
  });
});
