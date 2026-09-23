import { describe, it, expect, afterEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import i18n from "../../lib/i18n";
import { AuthProvider } from "../../auth/AuthProvider";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch } from "../../lib/apiClient";
import { pollerStatus } from "../../lib/mockData";
import { PollerBanner } from "./PollerBanner";

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

afterEach(async () => {
  pollerStatus[0].status = "active";
  pollerStatus[0].last_error = null;
  await apiFetch("/api/auth/logout", { method: "POST" });
});

function renderBanner() {
  return render(
    <MemoryRouter>
      <TestQueryProvider>
        <AuthProvider>
          <PollerBanner />
        </AuthProvider>
      </TestQueryProvider>
    </MemoryRouter>
  );
}

describe("PollerBanner", () => {
  it("renders nothing when all integrations are active", async () => {
    await loginAsOwner();
    renderBanner();
    await new Promise((r) => setTimeout(r, 500));
    expect(screen.queryByTestId("poller-banner")).not.toBeInTheDocument();
  });

  it("a simulated failure in one integration shows the banner", async () => {
    pollerStatus[0].status = "invalid";
    pollerStatus[0].last_error = "Credenciais inválidas";
    await loginAsOwner();
    renderBanner();

    expect(await screen.findByTestId("poller-banner")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Ver detalhes" })).toBeInTheDocument();
  });

  it("renders in English when the active language is en", async () => {
    pollerStatus[0].status = "invalid";
    pollerStatus[0].last_error = "Credenciais inválidas";
    await loginAsOwner();
    await i18n.changeLanguage("en");

    try {
      renderBanner();

      expect(await screen.findByTestId("poller-banner")).toBeInTheDocument();
      expect(screen.getByRole("button", { name: "View details" })).toBeInTheDocument();
    } finally {
      await i18n.changeLanguage("pt-BR");
    }
  });
});
