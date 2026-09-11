import { describe, it, expect, afterEach, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { Toaster } from "sonner";
import { http, HttpResponse } from "msw";
import "../../lib/i18n";
import { AuthProvider } from "../../auth/AuthProvider";
import { TestQueryProvider } from "../../test/queryClient";
import { server } from "../../test/msw/server";
import { apiFetch } from "../../lib/apiClient";
import { PersonalInfoCard } from "./PersonalInfoCard";

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
    <MemoryRouter>
      <TestQueryProvider>
        <AuthProvider>
          <Toaster />
          <PersonalInfoCard />
        </AuthProvider>
      </TestQueryProvider>
    </MemoryRouter>
  );
}

describe("PersonalInfoCard", () => {
  it("exibe nome, email somente leitura e iniciais", async () => {
    await loginAsOwner();
    renderCard();

    const nameInput = (await screen.findByLabelText("Nome")) as HTMLInputElement;
    expect(nameInput.value).toBe("Ana Owner");
    expect(screen.getByLabelText("Email")).toHaveValue("owner@vane.app");
    expect(screen.getByLabelText("Email")).toHaveAttribute("readonly");
    expect(screen.getByTestId("profile-avatar")).toHaveTextContent("AO");
  });

  // PROFPAGE-04/05: salvar um nome válido envia o PATCH e mostra o toast; o
  // campo re-sincroniza com o valor re-hidratado de /me (o trim prova que o
  // estado veio do servidor, não do que foi digitado).
  it("salvar nome válido envia o PATCH e reflete o nome re-hidratado", async () => {
    const user = userEvent.setup();
    await loginAsOwner();
    renderCard();

    const nameInput = (await screen.findByLabelText("Nome")) as HTMLInputElement;
    await user.clear(nameInput);
    await user.type(nameInput, "  Ana Silva  ");
    await user.click(screen.getByRole("button", { name: "Salvar alterações" }));

    expect(await screen.findByText("Nome atualizado.")).toBeInTheDocument();
    await waitFor(() => expect(nameInput.value).toBe("Ana Silva"));
  });

  // PROFPAGE-06: nome vazio/whitespace bloqueia o submit, mostra erro inline
  // e não envia nenhuma requisição.
  it("nome vazio bloqueia o submit e não envia PATCH", async () => {
    const user = userEvent.setup();
    await loginAsOwner();
    const patchSpy = vi.fn();
    server.use(
      http.patch("/api/auth/me", () => {
        patchSpy();
        return HttpResponse.json({ error: "name is required" }, { status: 422 });
      })
    );
    renderCard();

    const nameInput = (await screen.findByLabelText("Nome")) as HTMLInputElement;
    await user.clear(nameInput);
    await user.type(nameInput, "   ");
    await user.click(screen.getByRole("button", { name: "Salvar alterações" }));

    expect(await screen.findByText("Informe seu nome.")).toBeInTheDocument();
    expect(patchSpy).not.toHaveBeenCalled();
  });

  // spec.md edge case: 422 do servidor mostra o mesmo erro inline do caso
  // vazio.
  it("422 do servidor mostra o erro inline de nome", async () => {
    const user = userEvent.setup();
    await loginAsOwner();
    server.use(
      http.patch("/api/auth/me", () => HttpResponse.json({ error: "name is required" }, { status: 422 }))
    );
    renderCard();

    const nameInput = (await screen.findByLabelText("Nome")) as HTMLInputElement;
    await user.clear(nameInput);
    await user.type(nameInput, "Ana");
    await user.click(screen.getByRole("button", { name: "Salvar alterações" }));

    expect(await screen.findByText("Informe seu nome.")).toBeInTheDocument();
  });
});
