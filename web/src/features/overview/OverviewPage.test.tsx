import { describe, it, expect, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { http, HttpResponse, delay } from "msw";
import "../../lib/i18n";
import i18n from "../../lib/i18n";
import { TestQueryProvider } from "../../test/queryClient";
import { server } from "../../test/msw/server";
import { seedAuditLogEntries } from "../../test/msw/handlers";
import { apiFetch } from "../../lib/apiClient";
import { OverviewPage } from "./OverviewPage";
import type { AuditLogEntry, OverviewResponse } from "../../types/api";

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

afterEach(async () => {
  await i18n.changeLanguage("pt-BR");
});

describe("OverviewPage", () => {
  // SKEL-04/05: while /api/overview is loading, the page shows skeleton
  // cards (not the old "Carregando…" paragraph as visible content) inside
  // an aria-busy container that still carries the sr-only loading string.
  it("shows skeletons (not the text) while /api/overview is loading", async () => {
    server.use(
      http.get("/api/overview", async () => {
        await delay("infinite");
        return HttpResponse.json(emptyOverview());
      }),
    );
    await loginAsOwner();
    renderPage();

    const srText = await screen.findByText("Carregando…");
    expect(srText.className).toContain("sr-only");
    expect(srText.closest('[aria-busy="true"]')).toBeInTheDocument();
    expect(screen.getAllByTestId("skeleton").length).toBeGreaterThan(0);
  });

  // SKEL-06: once the fetch resolves, skeletons are gone and the real
  // summary cards take over - loading and loaded are mutually exclusive.
  // "Atividade recente do time" (recent-team-activity) runs its own
  // independent useRecentActivity query, so this also waits for its
  // empty-state text before asserting zero skeletons page-wide.
  it("removes the skeletons as soon as /api/overview finishes loading", async () => {
    await loginAsOwner();
    renderPage();

    await waitFor(() => expect(screen.getByTestId("overview-card-uptime")).toBeInTheDocument());
    await waitFor(() => expect(screen.getByText("Nenhuma atividade recente.")).toBeInTheDocument());
    expect(screen.queryAllByTestId("skeleton")).toHaveLength(0);
  });

  it("renders the 4 cards with the endpoint's real values", async () => {
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

  it("empty tenant shows '—' for uptime, 0 for counts and the empty incidents state", async () => {
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

  it("renders exactly 14 bars with an accessible tooltip (null becomes '—')", async () => {
    await loginAsOwner();
    renderPage();

    await waitFor(() => expect(screen.getByTestId("overview-bar-0")).toBeInTheDocument());
    const bars = screen.getAllByTestId(/^overview-bar-/);
    expect(bars).toHaveLength(14);
    expect(bars[0]).toHaveAttribute("aria-label", expect.stringContaining("—"));
    expect(bars[13]).toHaveAttribute("aria-label", expect.stringContaining("99.9%"));
  });

  it("lists recent incidents and the 'Ver todos' link points to /incidents", async () => {
    await loginAsOwner();
    renderPage();

    expect(await screen.findByText("Latência elevada no checkout")).toBeInTheDocument();
    expect(screen.getByText("Instabilidade no gateway")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Ver todos" })).toHaveAttribute("href", "/incidents");
  });

  it("shows an empty state when there are no recent incidents", async () => {
    server.use(
      http.get("/api/overview", () =>
        HttpResponse.json({ ...emptyOverview(), uptime_avg_30d: 100 }),
      ),
    );
    await loginAsOwner();
    renderPage();

    await waitFor(() => expect(screen.getByText("Nenhum incidente recente.")).toBeInTheDocument());
  });

  it("the 4 quick shortcuts point to the correct routes", async () => {
    await loginAsOwner();
    renderPage();

    await waitFor(() => expect(screen.getByTestId("overview-card-uptime")).toBeInTheDocument());
    expect(screen.getByRole("link", { name: "Adicionar serviço" })).toHaveAttribute("href", "/services");
    expect(screen.getByRole("link", { name: "Criar status page" })).toHaveAttribute("href", "/domains");
    expect(screen.getByRole("link", { name: "Convidar usuário" })).toHaveAttribute("href", "/admins");
    expect(screen.getByRole("link", { name: "Ver domínios" })).toHaveAttribute("href", "/domains");
  });

  it("shows the error state when the endpoint fails", async () => {
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

  // recent-team-activity T17: "Recent team activity" is wired to
  // GET /api/audit-log (ACTIVITY-09) instead of the deleted ACTIVITY_FEED
  // mock array.
  describe("Recent team activity (audit-log)", () => {
    function entry(overrides: Partial<AuditLogEntry>): AuditLogEntry {
      return {
        action: "invited",
        target_label: "novo@acme.health",
        actor_name: "Ana Silva",
        actor_deleted: false,
        created_at: "2026-09-10T12:00:00Z",
        ...overrides,
      };
    }

    it("renders real entries with the correct phrasing, including one entry with a null target_label", async () => {
      seedAuditLogEntries([
        entry({ action: "invited", target_label: "novo@acme.health", actor_name: "Ana Silva" }),
        entry({ action: "role_changed", target_label: null, actor_name: "Rafael Nunes" }),
      ]);
      await loginAsOwner();
      renderPage();

      expect(await screen.findByText(/convidou novo@acme.health/)).toBeInTheDocument();
      expect(screen.getByText("Ana Silva")).toBeInTheDocument();
      // ACTIVITY-11: null target_label omits the target clause gracefully,
      // never the literal "null"/"undefined".
      expect(screen.getByText("alterou um papel")).toBeInTheDocument();
      expect(screen.queryByText(/null/)).not.toBeInTheDocument();
      expect(screen.queryByText(/undefined/)).not.toBeInTheDocument();
    });

    it("shows the removed-user placeholder when actor_deleted is true", async () => {
      seedAuditLogEntries([entry({ actor_deleted: true, actor_name: "" })]);
      await loginAsOwner();
      renderPage();

      expect(await screen.findByText("Usuário removido")).toBeInTheDocument();
    });

    it("the active language changes the date format, it doesn't stay always in pt-BR", async () => {
      seedAuditLogEntries([entry({ created_at: "2026-03-05T14:30:00.000Z" })]);
      await loginAsOwner();
      const { unmount } = renderPage();
      const [ptBRTimestamp] = await screen.findAllByTestId("activity-timestamp");
      const ptBRText = ptBRTimestamp.textContent ?? "";
      unmount();

      await i18n.changeLanguage("en");
      renderPage();
      const [enTimestamp] = await screen.findAllByTestId("activity-timestamp");
      const enText = enTimestamp.textContent ?? "";

      expect(enText).not.toBe(ptBRText);
    });

    it("shows skeletons while /api/audit-log is loading", async () => {
      server.use(
        http.get("/api/audit-log", async () => {
          await delay("infinite");
          return HttpResponse.json([]);
        }),
      );
      await loginAsOwner();
      renderPage();

      await waitFor(() => expect(screen.getByTestId("overview-card-uptime")).toBeInTheDocument());
      expect(screen.getAllByTestId("skeleton").length).toBeGreaterThan(0);
    });

    it("shows the error row when /api/audit-log fails", async () => {
      server.use(
        http.get("/api/audit-log", () =>
          HttpResponse.json({ error: "internal server error" }, { status: 500 }),
        ),
      );
      await loginAsOwner();
      renderPage();

      await waitFor(() =>
        expect(screen.getAllByRole("alert").map((el) => el.textContent)).toContain(
          "Não foi possível carregar a atividade recente. Tente novamente.",
        ),
      );
    });

    it("shows the empty state when the tenant has no activity at all", async () => {
      await loginAsOwner();
      renderPage();

      expect(await screen.findByText("Nenhuma atividade recente.")).toBeInTheDocument();
    });
  });
});
