import { describe, it, expect, afterEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { Toaster } from "sonner";
import { http, HttpResponse } from "msw";
import i18n from "../../lib/i18n";
import { AuthProvider } from "../../auth/AuthProvider";
import { TestQueryProvider } from "../../test/queryClient";
import { server } from "../../test/msw/server";
import { apiFetch } from "../../lib/apiClient";
import { setDeploymentMode, resetDeploymentMode } from "../../test/msw/handlers";
import { BillingPage } from "./BillingPage";

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

afterEach(async () => {
  resetDeploymentMode();
  try {
    await apiFetch("/api/auth/logout", { method: "POST" });
  } catch {
    /* ignore */
  }
});

function renderPage() {
  return render(
    <TestQueryProvider>
      <MemoryRouter initialEntries={["/billing"]}>
        <AuthProvider>
          <Toaster />
          <BillingPage />
        </AuthProvider>
      </MemoryRouter>
    </TestQueryProvider>,
  );
}

function mockMembershipPlan(planTier: string) {
  server.use(
    http.get("/api/auth/me", () =>
      HttpResponse.json({
        id: "admin-1",
        name: "Ana Silva",
        email: "owner@vane.app",
        role: "owner",
        active_tenant_id: "tenant-1",
        two_factor_enabled: false,
        memberships: [{ tenant_id: "tenant-1", role: "owner", name: "Acme Health", plan_tier: planTier }],
      }),
    ),
  );
}

describe("BillingPage - BILLPG-01..06", () => {
  it("saas mode: shows the current plan and the 3 plan cards", async () => {
    setDeploymentMode("saas");
    mockMembershipPlan("starter");
    await loginAsOwner();
    renderPage();

    expect(await screen.findByText("Starter")).toBeInTheDocument();
    expect(screen.getByTestId("plan-card-free")).toBeInTheDocument();
    expect(screen.getByTestId("plan-card-starter")).toBeInTheDocument();
    expect(screen.getByTestId("plan-card-scale")).toBeInTheDocument();
    expect(screen.getByTestId("plan-card-starter-button")).toBeDisabled();
    expect(screen.getByTestId("plan-card-free-button")).not.toBeDisabled();
    expect(screen.getByTestId("plan-card-scale-button")).not.toBeDisabled();
  });

  it("saas mode: payment method and invoices always show the empty state", async () => {
    setDeploymentMode("saas");
    mockMembershipPlan("free");
    await loginAsOwner();
    renderPage();

    await screen.findByText("Forma de pagamento");
    expect(screen.getByText(/Nenhum cartão cadastrado/)).toBeInTheDocument();
    expect(screen.getByText(/Suas faturas aparecerão aqui/)).toBeInTheDocument();
  });

  it("saas mode: clicking Upgrade shows a decorative toast, with no checkout UI or card field", async () => {
    setDeploymentMode("saas");
    mockMembershipPlan("free");
    await loginAsOwner();
    renderPage();

    await userEvent.click(await screen.findByTestId("plan-card-scale-button"));

    expect(await screen.findByText("Em breve")).toBeInTheDocument();
    expect(document.querySelector('input[placeholder*="1234"]')).not.toBeInTheDocument();
    expect(screen.queryByText("Informações de pagamento")).not.toBeInTheDocument();
  });

  it("self_hosted mode: shows the inactive-license banner and the 2 license cards", async () => {
    setDeploymentMode("self_hosted");
    await loginAsOwner();
    renderPage();

    expect(await screen.findByText("Nenhuma licença ativa")).toBeInTheDocument();
    expect(screen.getByText("Recursos padrão bloqueados")).toBeInTheDocument();
    expect(screen.getByTestId("license-buy-button")).toBeInTheDocument();
    expect(screen.getByTestId("license-activate-button")).toBeInTheDocument();
  });

  it("self_hosted mode: buy/activate license show a decorative toast, without actually activating anything", async () => {
    setDeploymentMode("self_hosted");
    await loginAsOwner();
    renderPage();

    await userEvent.click(await screen.findByTestId("license-buy-button"));
    expect(await screen.findByText("Em breve")).toBeInTheDocument();

    // Still inactive after "buying" - no client-side state change.
    expect(screen.getByText("Nenhuma licença ativa")).toBeInTheDocument();
  });

  // BILLPG-04's own non-negotiable: the license-key field is decorative
  // only, never a real activation input - disabled proves it can't even be
  // typed into, not just that its button is a no-op.
  it("self_hosted mode: license key field is purely decorative (disabled)", async () => {
    setDeploymentMode("self_hosted");
    await loginAsOwner();
    renderPage();

    expect(await screen.findByLabelText("Código de licença")).toBeDisabled();
  });

  it("current-plan banner (BillingPage) reflects the real plan_tier, not a fixed value", async () => {
    setDeploymentMode("saas");
    mockMembershipPlan("scale");
    await loginAsOwner();
    renderPage();

    // This asserts BillingPage's own current-plan banner - AC5's sidebar
    // (TenantSwitcher) plan badge is separately covered by
    // TenantSwitcher.test.tsx, not by this test.
    expect(await screen.findByText("Scale")).toBeInTheDocument();
  });

  it("renders in English when the active language is en", async () => {
    setDeploymentMode("saas");
    mockMembershipPlan("starter");
    await loginAsOwner();
    await i18n.changeLanguage("en");

    try {
      renderPage();

      expect(await screen.findByText("Plans & Billing")).toBeInTheDocument();
      expect(screen.getByText("Upgrade")).toBeInTheDocument();
    } finally {
      await i18n.changeLanguage("pt-BR");
    }
  });
});
