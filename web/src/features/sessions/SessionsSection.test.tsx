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
import { SessionsSection } from "./SessionsSection";

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

afterEach(async () => {
  await apiFetch("/api/auth/logout", { method: "POST" });
});

function renderSection() {
  return render(
    <MemoryRouter>
      <TestQueryProvider>
        <AuthProvider>
          <Toaster />
          <SessionsSection />
        </AuthProvider>
      </TestQueryProvider>
    </MemoryRouter>
  );
}

describe("SessionsSection", () => {
  it("renderiza título e subtítulo em pt-BR", async () => {
    await loginAsOwner();
    renderSection();
    expect(await screen.findByText("Sessões ativas")).toBeInTheDocument();
    expect(screen.getByText(/dispositivos conectados à sua conta/i)).toBeInTheDocument();
  });

  it("lista as sessões do usuário logado, marca a atual com badge e esconde o botão Encerrar nela", async () => {
    await loginAsOwner();
    renderSection();

    const rows = await screen.findAllByTestId("session-row");
    expect(rows).toHaveLength(2);

    const currentRow = rows.find((r) => r.dataset.current === "true");
    const otherRow = rows.find((r) => r.dataset.current === "false");
    expect(currentRow).toBeDefined();
    expect(otherRow).toBeDefined();

    // A sessão atual carrega o badge "Esta sessão" e não expõe o botão.
    expect(currentRow!.querySelector('[data-testid="session-current-badge"]')).toHaveTextContent(
      "Esta sessão"
    );
    expect(currentRow!.querySelector('[data-testid="revoke-button"]')).toBeNull();

    // A outra sessão tem o botão Encerrar.
    const revokeButton = otherRow!.querySelector(
      '[data-testid="revoke-button"]'
    ) as HTMLButtonElement;
    expect(revokeButton).toBeInTheDocument();
    expect(revokeButton).toHaveTextContent("Encerrar");
  });

  it("clicar em Encerrar abre o diálogo e confirmar revoga a sessão", async () => {
    const user = userEvent.setup();
    await loginAsOwner();
    renderSection();

    const rows = await screen.findAllByTestId("session-row");
    expect(rows).toHaveLength(2);

    const otherRow = rows.find((r) => r.dataset.current === "false")!;
    const revokeButton = otherRow.querySelector(
      '[data-testid="revoke-button"]'
    ) as HTMLButtonElement;
    await user.click(revokeButton);

    // Só o confirmar do diálogo dispara o DELETE; a lista ainda tem 2 linhas
    // enquanto o diálogo está aberto.
    await user.click(await screen.findByTestId("confirm-revoke-button"));

    // Mock: DELETE marca revoked_at em sess-2; onSuccess invalida a query
    // ["sessions"]; o refetch filtra a linha revogada e a UI re-renderiza
    // com apenas a sessão atual.
    await waitFor(() => expect(screen.getAllByTestId("session-row")).toHaveLength(1));
    expect(screen.getByTestId("session-row").dataset.current).toBe("true");
  });

  // PROFPAGE-20: abrir o diálogo não envia DELETE - a revogação só acontece
  // no confirmar.
  it("abrir o diálogo de encerrar não envia DELETE até confirmar", async () => {
    const user = userEvent.setup();
    await loginAsOwner();
    const spy = vi.fn();
    server.use(
      http.delete("/api/auth/sessions/:id", () => {
        spy();
        return new HttpResponse(null, { status: 204 });
      })
    );
    renderSection();

    const rows = await screen.findAllByTestId("session-row");
    const otherRow = rows.find((r) => r.dataset.current === "false")!;
    await user.click(otherRow.querySelector('[data-testid="revoke-button"]') as HTMLButtonElement);

    expect(await screen.findByTestId("confirm-revoke-button")).toBeInTheDocument();
    expect(spy).not.toHaveBeenCalled();
  });

  // PROFPAGE-22: cancelar não envia DELETE e mantém a sessão listada.
  it("cancelar o diálogo não envia DELETE e mantém a sessão", async () => {
    const user = userEvent.setup();
    await loginAsOwner();
    const spy = vi.fn();
    server.use(
      http.delete("/api/auth/sessions/:id", () => {
        spy();
        return new HttpResponse(null, { status: 204 });
      })
    );
    renderSection();

    const rows = await screen.findAllByTestId("session-row");
    const otherRow = rows.find((r) => r.dataset.current === "false")!;
    await user.click(otherRow.querySelector('[data-testid="revoke-button"]') as HTMLButtonElement);
    await user.click(await screen.findByTestId("cancel-revoke-button"));

    expect(spy).not.toHaveBeenCalled();
    await waitFor(() => expect(screen.getAllByTestId("session-row")).toHaveLength(2));
  });

  it("renderiza o estado vazio quando o backend não retorna nenhuma sessão", async () => {
    server.use(http.get("/api/auth/sessions", () => HttpResponse.json([])));
    await loginAsOwner();
    renderSection();

    expect(await screen.findByTestId("sessions-empty")).toHaveTextContent("Nenhuma sessão ativa.");
  });

  it("renderiza a mensagem de erro de carregamento (não a de encerrar) quando o GET falha", async () => {
    server.use(
      http.get("/api/auth/sessions", () =>
        HttpResponse.json({ error: "boom" }, { status: 500 })
      )
    );
    await loginAsOwner();
    renderSection();

    expect(await screen.findByTestId("sessions-error")).toHaveTextContent(
      "Não foi possível carregar as sessões. Tente novamente."
    );
  });
});
