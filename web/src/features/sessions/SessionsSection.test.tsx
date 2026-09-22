import { describe, it, expect, afterEach, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { Toaster } from "sonner";
import { http, HttpResponse, delay } from "msw";
import "../../lib/i18n";
import i18n from "../../lib/i18n";
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
  await i18n.changeLanguage("pt-BR");
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
  it("renders the title and subtitle in pt-BR", async () => {
    await loginAsOwner();
    renderSection();
    expect(await screen.findByText("Sessões ativas")).toBeInTheDocument();
    expect(screen.getByText(/dispositivos conectados à sua conta/i)).toBeInTheDocument();
  });

  it("lists the logged-in user's sessions, badges the current one and hides its Encerrar button", async () => {
    await loginAsOwner();
    renderSection();

    const rows = await screen.findAllByTestId("session-row");
    expect(rows).toHaveLength(2);

    const currentRow = rows.find((r) => r.dataset.current === "true");
    const otherRow = rows.find((r) => r.dataset.current === "false");
    expect(currentRow).toBeDefined();
    expect(otherRow).toBeDefined();

    // The current session carries the "Esta sessão" badge and doesn't expose the button.
    expect(currentRow!.querySelector('[data-testid="session-current-badge"]')).toHaveTextContent(
      "Esta sessão"
    );
    expect(currentRow!.querySelector('[data-testid="revoke-button"]')).toBeNull();

    // The other session has the Encerrar button.
    const revokeButton = otherRow!.querySelector(
      '[data-testid="revoke-button"]'
    ) as HTMLButtonElement;
    expect(revokeButton).toBeInTheDocument();
    expect(revokeButton).toHaveTextContent("Encerrar");
  });

  it("the active language changes the session date format, not always pt-BR", async () => {
    await loginAsOwner();
    const { unmount } = renderSection();
    const [ptBRTimestamp] = await screen.findAllByTestId("session-last-seen");
    const ptBRText = ptBRTimestamp.textContent ?? "";
    unmount();

    await i18n.changeLanguage("en");
    renderSection();
    const [enTimestamp] = await screen.findAllByTestId("session-last-seen");
    const enText = enTimestamp.textContent ?? "";

    expect(enText).not.toBe(ptBRText);
  });

  it("clicking Encerrar opens the dialog and confirming revokes the session", async () => {
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

    // Only confirming the dialog fires the DELETE; the list still has 2
    // rows while the dialog is open.
    await user.click(await screen.findByTestId("confirm-revoke-button"));

    // Mock: DELETE sets revoked_at on sess-2; onSuccess invalidates the
    // ["sessions"] query; the refetch filters out the revoked row and the
    // UI re-renders with only the current session.
    await waitFor(() => expect(screen.getAllByTestId("session-row")).toHaveLength(1));
    expect(screen.getByTestId("session-row").dataset.current).toBe("true");
  });

  // PROFPAGE-20: opening the dialog does not send DELETE - revocation only
  // happens on confirm.
  it("opening the Encerrar dialog does not send DELETE until confirmed", async () => {
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

  // PROFPAGE-22: canceling does not send DELETE and keeps the session listed.
  it("canceling the dialog does not send DELETE and keeps the session", async () => {
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

  it("renders the empty state when the backend returns no sessions", async () => {
    server.use(http.get("/api/auth/sessions", () => HttpResponse.json([])));
    await loginAsOwner();
    renderSection();

    expect(await screen.findByTestId("sessions-empty")).toHaveTextContent("Nenhuma sessão ativa.");
  });

  // SKEL-04/05: while /api/auth/sessions is loading, skeleton rows render
  // (not the old "Carregando sessões..." paragraph as visible content)
  // inside an aria-busy container that still carries the sr-only string.
  it("shows skeletons (not the text) while /api/auth/sessions is loading", async () => {
    server.use(
      http.get("/api/auth/sessions", async () => {
        await delay("infinite");
        return HttpResponse.json([]);
      }),
    );
    await loginAsOwner();
    renderSection();

    const srText = await screen.findByText("Carregando sessões...");
    expect(srText.className).toContain("sr-only");
    expect(srText.closest('[aria-busy="true"]')).toBeInTheDocument();
    expect(screen.getAllByTestId("skeleton").length).toBeGreaterThan(0);
  });

  // SKEL-06/07: once the fetch resolves, skeletons are gone; the isError
  // branch stays untouched as verified by the existing error test below.
  it("removes the skeletons as soon as /api/auth/sessions finishes loading", async () => {
    await loginAsOwner();
    renderSection();

    await screen.findAllByTestId("session-row");
    expect(screen.queryAllByTestId("skeleton")).toHaveLength(0);
  });

  it("renders the loading error message (not the Encerrar one) when the GET fails", async () => {
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
