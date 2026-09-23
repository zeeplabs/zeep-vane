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
  // PROFRD-03: "Senha atual" full-width above, "Nova senha"/"Confirmar nova
  // senha" side by side in a 2-column grid - matching the mock's layout.
  it("Nova senha and Confirmar nova senha render in a 2-column grid (PROFRD-03)", async () => {
    await loginAsOwner();
    renderCard();

    const newPasswordInput = await screen.findByLabelText("Nova senha");
    const grid = newPasswordInput.closest(".grid");
    expect(grid).not.toBeNull();
    expect(grid!.className).toContain("grid-cols-2");
    expect(grid).toContainElement(screen.getByLabelText("Confirmar nova senha"));
    expect(grid).not.toContainElement(screen.getByLabelText("Senha atual"));
  });

  // PROFPAGE-08: a different confirmation blocks the submit and sends no request.
  it("different confirmation blocks the submit without sending a request", async () => {
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

  // PROFPAGE-09: 401 shows the current-password error.
  it("401 shows the current-password error", async () => {
    const user = userEvent.setup();
    await loginAsOwner();
    renderCard();

    await fill(user, "errada", "nova-senha-1", "nova-senha-1");

    expect(await screen.findByText("Senha atual incorreta.")).toBeInTheDocument();
  });

  // PROFPAGE-10: 422 shows the password policy error.
  it("422 shows the password policy error", async () => {
    const user = userEvent.setup();
    await loginAsOwner();
    renderCard();

    await fill(user, "demo1234", "curta", "curta");

    expect(await screen.findByText("A senha deve ter entre 8 e 72 caracteres.")).toBeInTheDocument();
  });

  // PROFPAGE-07: success clears the form and shows the toast.
  it("success clears the form and shows the toast", async () => {
    const user = userEvent.setup();
    await loginAsOwner();
    renderCard();

    await fill(user, "demo1234", "nova-senha-1", "nova-senha-1");

    expect(await screen.findByText("Senha atualizada.")).toBeInTheDocument();
    expect((screen.getByLabelText("Senha atual") as HTMLInputElement).value).toBe("");
    expect((screen.getByLabelText("Nova senha") as HTMLInputElement).value).toBe("");
    expect((screen.getByLabelText("Confirmar nova senha") as HTMLInputElement).value).toBe("");
    // PROFPAGE-11: the current session remains authenticated after the change.
    await expect(apiFetch<{ email: string }>("/api/auth/me")).resolves.toMatchObject({
      email: "owner@vane.app",
    });
  });
});
