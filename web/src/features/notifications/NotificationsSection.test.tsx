import { describe, it, expect, afterEach, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse, delay } from "msw";
import { toast } from "sonner";
import "../../lib/i18n";
import i18n from "../../lib/i18n";
import { TestQueryProvider } from "../../test/queryClient";
import { server } from "../../test/msw/server";
import { apiFetch } from "../../lib/apiClient";
import { NotificationsSection } from "./NotificationsSection";

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

function renderSection() {
  return render(
    <TestQueryProvider>
      <NotificationsSection />
    </TestQueryProvider>,
  );
}

afterEach(async () => {
  vi.restoreAllMocks();
  await i18n.changeLanguage("pt");
  await apiFetch("/api/auth/logout", { method: "POST" });
});

describe("NotificationsSection", () => {
  // NOTIFPREF-13: the three toggles reflect the values returned by the hook.
  it("renders the three toggles matching the stored preferences", async () => {
    await loginAsOwner();
    renderSection();

    expect(await screen.findByText("Novo incidente")).toBeInTheDocument();
    expect(screen.getByText("Incidente resolvido")).toBeInTheDocument();
    expect(screen.getByText("Resumo semanal")).toBeInTheDocument();

    await waitFor(() =>
      expect(screen.getByRole("switch", { name: "Novo incidente" })).toHaveAttribute(
        "aria-checked",
        "true",
      ),
    );
    expect(screen.getByRole("switch", { name: "Incidente resolvido" })).toHaveAttribute(
      "aria-checked",
      "true",
    );
    expect(screen.getByRole("switch", { name: "Resumo semanal" })).toHaveAttribute(
      "aria-checked",
      "false",
    );
  });

  // NOTIFPREF-15: while the initial request is in flight the switches are
  // disabled.
  it("disables the switches while the preferences request is loading", async () => {
    await loginAsOwner();
    server.use(
      http.get("/api/auth/notification-preferences", async () => {
        await delay("infinite");
        return HttpResponse.json({});
      }),
    );

    renderSection();

    const switchEl = await screen.findByRole("switch", { name: "Novo incidente" });
    expect(switchEl).toBeDisabled();
    expect(screen.getByRole("switch", { name: "Resumo semanal" })).toBeDisabled();
  });

  // NOTIFPREF-16: an initial-load failure shows an inline error without
  // breaking the rest of the card.
  it("shows an inline error and disables the switches when the load fails", async () => {
    await loginAsOwner();
    server.use(
      http.get("/api/auth/notification-preferences", () =>
        HttpResponse.json({ error: "boom" }, { status: 500 }),
      ),
    );

    renderSection();

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Não foi possível carregar as preferências. Tente novamente.",
    );
    expect(screen.getByRole("switch", { name: "Novo incidente" })).toBeDisabled();
  });

  // NOTIFPREF-14: flipping one switch sends a PATCH for that single key.
  it("sends a single-key PATCH when a toggle is flipped", async () => {
    await loginAsOwner();
    let capturedBody: Record<string, boolean> | null = null;
    server.use(
      http.patch("/api/auth/notification-preferences", async ({ request }) => {
        capturedBody = (await request.json()) as Record<string, boolean>;
        return HttpResponse.json({
          incident_opened: true,
          incident_resolved: true,
          weekly_digest: true,
        });
      }),
    );

    renderSection();
    const user = userEvent.setup();
    const digestSwitch = await screen.findByRole("switch", { name: "Resumo semanal" });
    await waitFor(() => expect(digestSwitch).not.toBeDisabled());

    await user.click(digestSwitch);

    await waitFor(() => expect(capturedBody).toEqual({ weekly_digest: true }));
    await waitFor(() => expect(digestSwitch).toHaveAttribute("aria-checked", "true"));
  });

  // NOTIFPREF-14: a failed PATCH reverts the toggle and surfaces the toast.
  it("reverts the toggle and shows the error toast when the PATCH fails", async () => {
    await loginAsOwner();
    const errorSpy = vi.spyOn(toast, "error").mockImplementation(() => "");
    server.use(
      http.patch("/api/auth/notification-preferences", () =>
        HttpResponse.json({ error: "boom" }, { status: 500 }),
      ),
    );

    renderSection();
    const user = userEvent.setup();
    const digestSwitch = await screen.findByRole("switch", { name: "Resumo semanal" });
    await waitFor(() => expect(digestSwitch).not.toBeDisabled());

    await user.click(digestSwitch);

    await waitFor(() =>
      expect(errorSpy).toHaveBeenCalledWith(
        "Não foi possível salvar a preferência. Tente novamente.",
      ),
    );
    await waitFor(() => expect(digestSwitch).toHaveAttribute("aria-checked", "false"));
  });

  // L-043 / spec locale: every new string renders in English under the en
  // locale.
  it("renders the section in English when the locale is en", async () => {
    await loginAsOwner();
    await i18n.changeLanguage("en");
    renderSection();

    expect(await screen.findByText("Notifications")).toBeInTheDocument();
    expect(screen.getByText("New incident")).toBeInTheDocument();
    expect(screen.getByText("Incident resolved")).toBeInTheDocument();
    expect(screen.getByText("Weekly digest")).toBeInTheDocument();
  });
});
