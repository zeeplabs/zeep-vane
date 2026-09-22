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
  it("valid submission with matching passwords shows success confirmation", async () => {
    seedPasswordResetToken("valid-token", "owner@vane.app");
    render(App("valid-token"));

    await fillForm("demo1234", "demo1234");
    await userEvent.click(screen.getByRole("button", { name: "Redefinir senha" }));

    expect(await screen.findByText("Senha redefinida com sucesso.")).toBeInTheDocument();
  });

  it("'Ir para o login' button navigates to /login after success", async () => {
    seedPasswordResetToken("valid-token", "owner@vane.app");
    render(App("valid-token"));

    await fillForm("demo1234", "demo1234");
    await userEvent.click(screen.getByRole("button", { name: "Redefinir senha" }));
    await screen.findByText("Senha redefinida com sucesso.");

    await userEvent.click(screen.getByRole("button", { name: "Ir para o login" }));
    expect(await screen.findByText("login page")).toBeInTheDocument();
  });

  it("mismatched password and confirmation show a validation error without calling the API", async () => {
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

  it("unknown/expired token (401) shows a generic message without locking the form", async () => {
    render(App("does-not-exist"));

    await fillForm("demo1234", "demo1234");
    await userEvent.click(screen.getByRole("button", { name: "Redefinir senha" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Este link de recuperação é inválido ou expirou. Solicite um novo.",
    );
    expect(screen.getByRole("button", { name: "Redefinir senha" })).not.toBeDisabled();
  });

  it("weak password (422) shows the translated message, not the raw server text", async () => {
    seedPasswordResetToken("valid-token", "owner@vane.app");
    render(App("valid-token"));

    await fillForm("short", "short");
    await userEvent.click(screen.getByRole("button", { name: "Redefinir senha" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "A senha deve ter entre 8 e 72 caracteres.",
    );
  });

  it("network failure shows a generic fallback message", async () => {
    server.use(http.post("/api/auth/password-reset/confirm", () => HttpResponse.error()));
    render(App("valid-token"));

    await fillForm("demo1234", "demo1234");
    await userEvent.click(screen.getByRole("button", { name: "Redefinir senha" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Não foi possível redefinir a senha. Tente novamente.",
    );
  });

  it("submit button is disabled during submission, preventing a double click", async () => {
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

  it("never sends the raw token in the body beyond the field the backend expects", async () => {
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

  it("password and confirmation fields are required", () => {
    render(App("valid-token"));

    expect(screen.getByLabelText("Nova senha")).toBeRequired();
    expect(screen.getByLabelText("Confirmar nova senha")).toBeRequired();
  });
});
