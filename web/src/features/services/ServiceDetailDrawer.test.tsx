import { describe, it, expect, afterEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { server } from "../../test/msw/server";
import "../../lib/i18n";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch } from "../../lib/apiClient";
import { ServiceDetailDrawer } from "./ServiceDetailDrawer";
import type { ServiceStatus } from "../../types/api";

function buildBuckets(count: number, status: "operational" | "degraded" | "outage" | "no_data" = "operational") {
  return Array.from({ length: count }, (_, i) => ({
    start: new Date(Date.now() - (count - 1 - i) * 60 * 60 * 1000).toISOString(),
    status,
  }));
}

interface DetailFixture {
  id: string;
  name: string;
  slo_id: string | null;
  slo_name: string | null;
  monitor_mode?: "slo" | "polling";
  poll_target?: string | null;
  current_status: ServiceStatus;
  last_status_change_at: string;
  uptime_30d: number | null;
  last_seen_at: string | null;
  status_analysis: string | null;
  incidents_30d: number;
  hourly_buckets: ReturnType<typeof buildBuckets>;
}

function mockDetail(overrides: Partial<DetailFixture> = {}) {
  const base: DetailFixture = {
    id: "svc-detail-1",
    name: "Checkout",
    slo_id: "slo-1",
    slo_name: "Checkout latência p95",
    current_status: "operational",
    last_status_change_at: new Date().toISOString(),
    uptime_30d: 99.5,
    last_seen_at: new Date().toISOString(),
    status_analysis: null,
    incidents_30d: 2,
    hourly_buckets: buildBuckets(24),
    ...overrides,
  };
  server.use(http.get("/api/services/:id", () => HttpResponse.json(base)));
  return base;
}

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

afterEach(async () => {
  await apiFetch("/api/auth/logout", { method: "POST" });
});

function renderDrawer(onClose: () => void = () => {}) {
  return render(
    <TestQueryProvider>
      <ServiceDetailDrawer serviceId="svc-detail-1" onClose={onClose} />
    </TestQueryProvider>
  );
}

describe("ServiceDetailDrawer", () => {
  it("mostra Uptime 30d, Última verificação e Incidentes (30d) (SVC-14)", async () => {
    const lastSeen = new Date("2026-01-05T10:00:00Z").toISOString();
    mockDetail({ uptime_30d: 99.87, last_seen_at: lastSeen, incidents_30d: 3 });
    await loginAsOwner();
    renderDrawer();

    expect(await screen.findByText("99.87%")).toBeInTheDocument();
    expect(screen.getByText(new Date(lastSeen).toLocaleString("pt-BR"))).toBeInTheDocument();
    expect(screen.getByText("3")).toBeInTheDocument();
  });

  it("renderiza a nota quando degraded e status_analysis não vazio (SVC-15)", async () => {
    mockDetail({ current_status: "degraded", status_analysis: "Latência acima do normal." });
    await loginAsOwner();
    renderDrawer();

    expect(await screen.findByText("Latência acima do normal.")).toBeInTheDocument();
  });

  it("não renderiza a nota quando degraded mas status_analysis é vazio (SVC-16)", async () => {
    mockDetail({ current_status: "degraded", status_analysis: null });
    await loginAsOwner();
    renderDrawer();

    await screen.findByText("Degradado");
    expect(screen.queryByText("Latência acima do normal.")).not.toBeInTheDocument();
  });

  it("não renderiza a nota quando status_analysis existe mas o status não é degraded (SVC-16)", async () => {
    mockDetail({ current_status: "operational", status_analysis: "Texto que não deveria aparecer." });
    await loginAsOwner();
    renderDrawer();

    await screen.findByText("Operacional");
    expect(screen.queryByText("Texto que não deveria aparecer.")).not.toBeInTheDocument();
  });

  it("renderiza exatamente 24 barras de histórico, mesmo com um fixture de tamanho diferente (SVC-17)", async () => {
    mockDetail({ hourly_buckets: buildBuckets(24) });
    await loginAsOwner();
    renderDrawer();

    await screen.findByText("Checkout");
    expect(screen.getAllByTestId("history-bar")).toHaveLength(24);
  });

  it("fecha ao clicar no controle de fechar (SVC-18)", async () => {
    mockDetail();
    await loginAsOwner();
    let closeCalls = 0;
    renderDrawer(() => {
      closeCalls += 1;
    });

    await userEvent.click(await screen.findByRole("button", { name: "Fechar" }));
    expect(closeCalls).toBe(1);
  });

  it("fecha ao clicar no backdrop (SVC-18)", async () => {
    mockDetail();
    await loginAsOwner();
    let closeCalls = 0;
    renderDrawer(() => {
      closeCalls += 1;
    });
    await screen.findByText("Checkout");

    const overlay = document.querySelector(".fixed.inset-0.z-40") as HTMLElement;
    expect(overlay).not.toBeNull();
    await userEvent.click(overlay);
    expect(closeCalls).toBe(1);
  });

  // manual-polling-monitoring T9: a polling-manual service has no
  // slo_name/slo_id at all - the subtitle must show poll_target, not
  // blank/undefined.
  it("mostra poll_target como subtítulo para um serviço monitor_mode=polling (T9)", async () => {
    mockDetail({
      slo_id: null,
      slo_name: null,
      monitor_mode: "polling",
      poll_target: "cache.acme.health:6379",
    });
    await loginAsOwner();
    renderDrawer();

    await screen.findByText("Checkout");
    expect(screen.getByText("cache.acme.health:6379")).toBeInTheDocument();
  });

  // Regression: an slo-mode service's subtitle is unchanged.
  it("mantém slo_name como subtítulo para um serviço monitor_mode=slo (T9 regressão)", async () => {
    mockDetail({ monitor_mode: "slo" });
    await loginAsOwner();
    renderDrawer();

    await screen.findByText("Checkout");
    expect(screen.getByText("Checkout latência p95")).toBeInTheDocument();
  });

  it("não renderiza 'Pausar monitoramento' nem 'Editar configuração' (SVC-19)", async () => {
    mockDetail();
    await loginAsOwner();
    renderDrawer();

    await screen.findByText("Checkout");
    expect(screen.queryByText(/Pausar monitoramento/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/Editar configuração/i)).not.toBeInTheDocument();
  });
});
