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
  it("modo saas: mostra o plano atual e os 3 cards de plano", async () => {
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

  it("modo saas: forma de pagamento e faturas sempre mostram o estado vazio", async () => {
    setDeploymentMode("saas");
    mockMembershipPlan("free");
    await loginAsOwner();
    renderPage();

    await screen.findByText("Forma de pagamento");
    expect(screen.getByText(/Nenhum cartão cadastrado/)).toBeInTheDocument();
    expect(screen.getByText(/Suas faturas aparecerão aqui/)).toBeInTheDocument();
  });

  it("modo saas: clicar em Fazer upgrade mostra toast decorativo, sem UI de checkout nem campo de cartão", async () => {
    setDeploymentMode("saas");
    mockMembershipPlan("free");
    await loginAsOwner();
    renderPage();

    await userEvent.click(await screen.findByTestId("plan-card-scale-button"));

    expect(await screen.findByText("Em breve")).toBeInTheDocument();
    expect(document.querySelector('input[placeholder*="1234"]')).not.toBeInTheDocument();
    expect(screen.queryByText("Informações de pagamento")).not.toBeInTheDocument();
  });

  it("modo self_hosted: mostra banner de licença inativa e os 2 cards de licença", async () => {
    setDeploymentMode("self_hosted");
    await loginAsOwner();
    renderPage();

    expect(await screen.findByText("Nenhuma licença ativa")).toBeInTheDocument();
    expect(screen.getByText("Recursos padrão bloqueados")).toBeInTheDocument();
    expect(screen.getByTestId("license-buy-button")).toBeInTheDocument();
    expect(screen.getByTestId("license-activate-button")).toBeInTheDocument();
  });

  it("modo self_hosted: comprar/ativar licença mostram toast decorativo, sem ativar nada de verdade", async () => {
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
  it("modo self_hosted: campo de código de licença é somente decorativo (disabled)", async () => {
    setDeploymentMode("self_hosted");
    await loginAsOwner();
    renderPage();

    expect(await screen.findByLabelText("Código de licença")).toBeDisabled();
  });

  it("banner de plano atual (BillingPage) reflete o plan_tier real, não um valor fixo", async () => {
    setDeploymentMode("saas");
    mockMembershipPlan("scale");
    await loginAsOwner();
    renderPage();

    // This asserts BillingPage's own current-plan banner - AC5's sidebar
    // (TenantSwitcher) plan badge is separately covered by
    // TenantSwitcher.test.tsx, not by this test.
    expect(await screen.findByText("Scale")).toBeInTheDocument();
  });

  it("renderiza em inglês quando o idioma ativo é en", async () => {
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
