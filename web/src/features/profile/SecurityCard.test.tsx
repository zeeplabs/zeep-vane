import { describe, it, expect, afterEach, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Toaster } from "sonner";
import { http, HttpResponse } from "msw";
import "../../lib/i18n";
import { AuthProvider } from "../../auth/AuthProvider";
import { TestQueryProvider } from "../../test/queryClient";
import { server } from "../../test/msw/server";
import { apiFetch } from "../../lib/apiClient";
import { SecurityCard } from "./SecurityCard";

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

afterEach(async () => {
  await apiFetch("/api/auth/logout", { method: "POST" });
});

function renderCard() {
  return render(
    <TestQueryProvider>
      <AuthProvider>
        <Toaster />
        <SecurityCard />
      </AuthProvider>
    </TestQueryProvider>
  );
}

async function fill(user: ReturnType<typeof userEvent.setup>, current: string, next: string, confirmation: string) {
  await user.type(screen.getByLabelText("Senha atual"), current);
  await user.type(screen.getByLabelText("Nova senha"), next);
  await user.type(screen.getByLabelText("Confirmar nova senha"), confirmation);
  await user.click(screen.getByRole("button", { name: "Atualizar senha" }));
}

describe("SecurityCard", () => {
  // PROFPAGE-08: confirmação diferente bloqueia o submit e não envia request.
  it("confirmação diferente bloqueia o submit sem enviar request", async () => {
    const user = userEvent.setup();
    await loginAsOwner();
    const spy = vi.fn();
    server.use(
      http.post("/api/auth/change-password", () => {
        spy();
        return HttpResponse.json({ status: "ok" });
      })
    );
    renderCard();

    await fill(user, "demo1234", "nova-senha-1", "diferente-2");

    expect(await screen.findByText("As senhas não coincidem.")).toBeInTheDocument();
    expect(spy).not.toHaveBeenCalled();
  });

  // PROFPAGE-09: 401 mostra o erro de senha atual.
  it("401 mostra o erro de senha atual", async () => {
    const user = userEvent.setup();
    await loginAsOwner();
    renderCard();

    await fill(user, "errada", "nova-senha-1", "nova-senha-1");

    expect(await screen.findByText("Senha atual incorreta.")).toBeInTheDocument();
  });

  // PROFPAGE-10: 422 mostra o erro de política de senha.
  it("422 mostra o erro de política de senha", async () => {
    const user = userEvent.setup();
    await loginAsOwner();
    renderCard();

    await fill(user, "demo1234", "curta", "curta");

    expect(await screen.findByText("A senha deve ter entre 8 e 72 caracteres.")).toBeInTheDocument();
  });

  // PROFPAGE-07: sucesso limpa o form e mostra o toast.
  it("sucesso limpa o form e mostra o toast", async () => {
    const user = userEvent.setup();
    await loginAsOwner();
    renderCard();

    await fill(user, "demo1234", "nova-senha-1", "nova-senha-1");

    expect(await screen.findByText("Senha atualizada.")).toBeInTheDocument();
    expect((screen.getByLabelText("Senha atual") as HTMLInputElement).value).toBe("");
    expect((screen.getByLabelText("Nova senha") as HTMLInputElement).value).toBe("");
    expect((screen.getByLabelText("Confirmar nova senha") as HTMLInputElement).value).toBe("");
    // PROFPAGE-11: a sessão atual continua autenticada após a troca.
    await expect(apiFetch<{ email: string }>("/api/auth/me")).resolves.toMatchObject({
      email: "owner@vane.app",
    });
  });
});
