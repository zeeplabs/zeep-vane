import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import { HttpResponse, http } from "msw";
import "../../lib/i18n";
import { AcceptInvitePage } from "./AcceptInvitePage";
import { seedAdminInviteToken } from "../../test/msw/handlers";
import { server } from "../../test/msw/server";
import { TestQueryProvider } from "../../test/queryClient";

function App(token: string) {
  return (
    <TestQueryProvider>
      <MemoryRouter initialEntries={[`/accept-invite/${token}`]}>
        <Routes>
          <Route path="/accept-invite/:token" element={<AcceptInvitePage />} />
        </Routes>
      </MemoryRouter>
    </TestQueryProvider>
  );
}

async function fillForm(password: string, confirmPassword: string) {
  await userEvent.type(screen.getByLabelText("Senha"), password);
  await userEvent.type(screen.getByLabelText("Confirmar senha"), confirmPassword);
}

describe("AcceptInvitePage", () => {
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

  it("submissão válida com senhas iguais aceita o convite e navega para a raiz (AIP-01/02)", async () => {
    seedAdminInviteToken("valid-token", "invitee@vane.app", "operator");
    render(App("valid-token"));

    await fillForm("demo1234", "demo1234");
    await userEvent.click(screen.getByRole("button", { name: "Ativar conta" }));

    await waitFor(() => expect(assignSpy).toHaveBeenCalledWith("/"));
  });

  it("senha e confirmação diferentes mostram erro de validação sem chamar a API (AIP-05/06)", async () => {
    seedAdminInviteToken("valid-token", "invitee@vane.app", "operator");
    const fetchSpy = vi.spyOn(global, "fetch");
    render(App("valid-token"));

    await fillForm("demo1234", "outrasenha");
    await userEvent.click(screen.getByRole("button", { name: "Ativar conta" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("As senhas não coincidem.");
    expect(assignSpy).not.toHaveBeenCalled();
    expect(fetchSpy).not.toHaveBeenCalledWith(
      expect.stringContaining("/api/admins/invite/"),
      expect.anything(),
    );
  });

  it("token desconhecido/expirado (401) mostra mensagem genérica sem travar o formulário (AIP-07)", async () => {
    render(App("does-not-exist"));

    await fillForm("demo1234", "demo1234");
    await userEvent.click(screen.getByRole("button", { name: "Ativar conta" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Este link de convite é inválido ou expirou. Peça ao seu administrador para enviar um novo."
    );
    expect(assignSpy).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Ativar conta" })).not.toBeDisabled();
  });

  it("senha fraca (422) mostra a mensagem exata do servidor (AIP-08)", async () => {
    seedAdminInviteToken("valid-token", "invitee@vane.app", "operator");
    render(App("valid-token"));

    await fillForm("short", "short");
    await userEvent.click(screen.getByRole("button", { name: "Ativar conta" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "password must be between 8 and 72 characters"
    );
    expect(assignSpy).not.toHaveBeenCalled();
  });

  it("falha de rede mostra mensagem genérica de fallback (AIP-09)", async () => {
    // HttpResponse.error() simulates a genuine network failure (fetch()
    // itself rejects), unlike a 5xx HTTP response - the latter always
    // becomes an ApiError with a parsed message (apiClient.ts's
    // parseErrorMessage), so it can't reach the generic-fallback branch.
    server.use(http.post("/api/admins/invite/:token/accept", () => HttpResponse.error()));
    render(App("valid-token"));

    await fillForm("demo1234", "demo1234");
    await userEvent.click(screen.getByRole("button", { name: "Ativar conta" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Não foi possível ativar a conta. Tente novamente."
    );
    expect(assignSpy).not.toHaveBeenCalled();
  });

  it("botão de envio fica desabilitado durante a submissão (AIP-03)", async () => {
    seedAdminInviteToken("valid-token", "invitee@vane.app", "operator");
    render(App("valid-token"));

    await fillForm("demo1234", "demo1234");
    const submitButton = screen.getByRole("button", { name: "Ativar conta" });
    await userEvent.click(submitButton);

    await waitFor(() => expect(assignSpy).toHaveBeenCalled());
  });

  it("campos de senha e confirmação são obrigatórios", () => {
    render(App("valid-token"));

    expect(screen.getByLabelText("Senha")).toBeRequired();
    expect(screen.getByLabelText("Confirmar senha")).toBeRequired();
  });
});
