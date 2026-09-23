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
  it("displays name, read-only email, and initials", async () => {
    await loginAsOwner();
    renderCard();

    const nameInput = (await screen.findByLabelText("Nome")) as HTMLInputElement;
    expect(nameInput.value).toBe("Ana Owner");
    expect(screen.getByLabelText("Email")).toHaveValue("owner@vane.app");
    expect(screen.getByLabelText("Email")).toHaveAttribute("readonly");
    expect(screen.getByTestId("profile-avatar")).toHaveTextContent("AO");
  });

  // PROFRD-03: Nome/Email render side by side in a 2-column grid, matching
  // the mock's layout - not stacked in a single column.
  it("Nome and Email render in a 2-column grid (PROFRD-03)", async () => {
    await loginAsOwner();
    renderCard();

    const nameInput = await screen.findByLabelText("Nome");
    const grid = nameInput.closest(".grid");
    expect(grid).not.toBeNull();
    expect(grid!.className).toContain("grid-cols-2");
    expect(grid).toContainElement(screen.getByLabelText("Email"));
  });

  // PROFPAGE-04/05: saving a valid name sends the PATCH and shows the toast; the
  // field re-syncs with the re-hydrated value from /me (the trim proves the
  // state came from the server, not from what was typed).
  it("saving a valid name sends the PATCH and reflects the re-hydrated name", async () => {
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

  // PROFPAGE-06: an empty/whitespace name blocks the submit, shows an inline
  // error, and sends no request.
  it("empty name blocks the submit and sends no PATCH", async () => {
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

  // spec.md edge case: a 422 from the server shows the same inline error as
  // the empty case.
  it("422 from the server shows the inline name error", async () => {
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
