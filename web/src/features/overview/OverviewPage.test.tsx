import { describe, it, expect } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { http, HttpResponse } from "msw";
import "../../lib/i18n";
import { TestQueryProvider } from "../../test/queryClient";
import { server } from "../../test/msw/server";
import { apiFetch } from "../../lib/apiClient";
import { OverviewPage } from "./OverviewPage";
import type { OverviewResponse } from "../../types/api";

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

function renderPage() {
  return render(
    <TestQueryProvider>
      <MemoryRouter initialEntries={["/overview"]}>
        <OverviewPage />
      </MemoryRouter>
    </TestQueryProvider>,
  );
}

function emptyOverview(): OverviewResponse {
  return {
    uptime_avg_30d: null,
    uptime_avg_30d_prior: null,
    open_incidents: 0,
    open_incidents_critical: 0,
    open_incidents_monitoring: 0,
    unhealthy_services: 0,
    total_services: 0,
    verified_domains: 0,
    total_domains: 0,
    uptime_series: Array.from({ length: 14 }, (_, i) => ({
      date: `2026-01-${String(i + 1).padStart(2, "0")}`,
      uptime_percent: null,
    })),
    recent_incidents: [],
  };
}

describe("OverviewPage", () => {
  it("renderiza os 4 cards com os valores reais do endpoint", async () => {
    await loginAsOwner();
    renderPage();

    await waitFor(() => expect(screen.getByTestId("overview-card-uptime")).toBeInTheDocument());
    expect(screen.getByTestId("overview-card-uptime")).toHaveTextContent("99.9%");
    expect(screen.getByTestId("overview-card-open-incidents")).toHaveTextContent("1");
    expect(screen.getByTestId("overview-card-unhealthy")).toHaveTextContent("2");
    expect(screen.getByTestId("overview-card-domains")).toHaveTextContent("1/2");
    // OVW-17/18/19/20: card subtexts backed by real fields (2026-09-14
    // reversal of spec.md's original "no severity breakdown" call).
    expect(screen.getByText("+0.1% vs mês anterior")).toBeInTheDocument();
    expect(screen.getByText("1 crítico, 0 monitorando")).toBeInTheDocument();
    expect(screen.getByText("de 6 serviços monitorados")).toBeInTheDocument();
    expect(screen.getByText("1 pendente de verificação")).toBeInTheDocument();
    // 2026-09-14 decision: activity feed is a static placeholder (no
    // ActivityEvent model yet), always rendered.
    expect(screen.getByText(/Atividade recente do time/i)).toBeInTheDocument();
    // 2026-09-15: upsell banner disabled (no Tenant.Plan/billing behind it)
    // - must never render.
    expect(screen.queryByText(/plano Free/i)).not.toBeInTheDocument();
  });

  it("tenant vazio mostra '—' no uptime, 0 nos counts e o estado vazio de incidentes", async () => {
    server.use(http.get("/api/overview", () => HttpResponse.json(emptyOverview())));
    await loginAsOwner();
    renderPage();

    await waitFor(() => expect(screen.getByTestId("overview-card-uptime")).toBeInTheDocument());
    expect(screen.getByTestId("overview-card-uptime")).toHaveTextContent("—");
    expect(screen.getByTestId("overview-card-open-incidents")).toHaveTextContent("0");
    expect(screen.getByTestId("overview-card-unhealthy")).toHaveTextContent("0");
    expect(screen.getByTestId("overview-card-domains")).toHaveTextContent("0/0");
    expect(screen.getByText("Nenhum incidente recente.")).toBeInTheDocument();
    // OVW-17/18/20: no trend/breakdown/pending subtext when there's nothing
    // to compare or report - only the unconditional denominator (OVW-19)
    // still renders.
    expect(screen.queryByText(/vs mês anterior/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/crítico/i)).not.toBeInTheDocument();
    expect(screen.getByText("de 0 serviços monitorados")).toBeInTheDocument();
    expect(screen.queryByText(/pendente de verificação/i)).not.toBeInTheDocument();
  });

  it("renderiza exatamente 14 barras com tooltip acessível (null vira '—')", async () => {
    await loginAsOwner();
    renderPage();

    await waitFor(() => expect(screen.getByTestId("overview-bar-0")).toBeInTheDocument());
    const bars = screen.getAllByTestId(/^overview-bar-/);
    expect(bars).toHaveLength(14);
    expect(bars[0]).toHaveAttribute("aria-label", expect.stringContaining("—"));
    expect(bars[13]).toHaveAttribute("aria-label", expect.stringContaining("99.9%"));
  });

  it("lista incidentes recentes e o link 'Ver todos' aponta para /incidents", async () => {
    await loginAsOwner();
    renderPage();

    expect(await screen.findByText("Latência elevada no checkout")).toBeInTheDocument();
    expect(screen.getByText("Instabilidade no gateway")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Ver todos" })).toHaveAttribute("href", "/incidents");
  });

  it("mostra estado vazio quando não há incidentes recentes", async () => {
    server.use(
      http.get("/api/overview", () =>
        HttpResponse.json({ ...emptyOverview(), uptime_avg_30d: 100 }),
      ),
    );
    await loginAsOwner();
    renderPage();

    await waitFor(() => expect(screen.getByText("Nenhum incidente recente.")).toBeInTheDocument());
  });

  it("os 4 atalhos rápidos apontam para as rotas corretas", async () => {
    await loginAsOwner();
    renderPage();

    await waitFor(() => expect(screen.getByTestId("overview-card-uptime")).toBeInTheDocument());
    expect(screen.getByRole("link", { name: "Adicionar serviço" })).toHaveAttribute("href", "/services");
    expect(screen.getByRole("link", { name: "Criar status page" })).toHaveAttribute("href", "/domains");
    expect(screen.getByRole("link", { name: "Convidar usuário" })).toHaveAttribute("href", "/admins");
    expect(screen.getByRole("link", { name: "Ver domínios" })).toHaveAttribute("href", "/domains");
  });

  it("mostra o estado de erro quando o endpoint falha", async () => {
    server.use(
      http.get("/api/overview", () =>
        HttpResponse.json({ error: "internal server error" }, { status: 500 }),
      ),
    );
    await loginAsOwner();
    renderPage();

    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent(
        "Não foi possível carregar o resumo. Tente novamente.",
      ),
    );
  });
});
