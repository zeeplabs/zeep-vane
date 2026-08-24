import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { server } from "../../test/msw/server";
import { apiFetch } from "../../lib/apiClient";
import { PublicStatusPage } from "./PublicStatusPage";
import type { PublicHourlyStatus } from "../../lib/publicStatus";

// The preview endpoint (I12) sits behind requireAuth - unlike the real
// production public page (served by the Go backend directly via Host
// header, never through this SPA route). Any authenticated role can
// preview, so a fixed owner login is enough setup for every case here.
async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

async function renderAt(path: string) {
  await loginAsOwner();
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/status/:id" element={<PublicStatusPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

function bucket(startIso: string, status: PublicHourlyStatus) {
  return { start: startIso, status };
}

// mockPublicPreview overrides the public-preview MSW handler with a single
// service carrying an explicit hourly_history, so tests can assert exact
// colors/tooltips instead of the generic fixture in test/msw/handlers.ts.
function mockPublicPreview(serviceName: string, hourlyHistory: ReturnType<typeof bucket>[]) {
  server.use(
    http.get("/api/status-pages/:id/public-preview", () =>
      HttpResponse.json({
        company: { name: "Acme Status", logo_url: null },
        services: [
          {
            name: serviceName,
            status: "operational",
            last_updated_at: new Date().toISOString(),
            hourly_history: hourlyHistory,
          },
        ],
        incidents: { active: [], resolved: [] },
      }),
    ),
  );
}

function hourlyBars(serviceName: string) {
  return screen.getAllByTestId(new RegExp(`^hourly-bar-${serviceName}-\\d+$`));
}

describe("PublicStatusPage", () => {
  it("página sem incidentes mostra banda de operacional e nenhum incidente ativo", async () => {
    await renderAt("/status/sp-4");

    expect(await screen.findByText("Todos os sistemas operacionais")).toBeInTheDocument();
    expect(screen.getByText("Fila de processamento")).toBeInTheDocument();
    expect(screen.queryByText("Incidente em andamento")).not.toBeInTheDocument();
    expect(await screen.findByText("Nenhum incidente nos últimos 90 dias.")).toBeInTheDocument();
  });

  it("página com incidente ativo mostra card no topo e permite expandir a linha do tempo", async () => {
    await renderAt("/status/sp-1");

    expect(await screen.findByText("Incidente em andamento")).toBeInTheDocument();
    expect(screen.getByText("Latência elevada no Checkout")).toBeInTheDocument();

    const [toggleActiveTimeline] = screen.getAllByRole("button", { name: "Ver linha do tempo" });
    await userEvent.click(toggleActiveTimeline);
    expect(
      await screen.findByText("Causa raiz identificada: pico de tráfego não previsto. Monitorando estabilização."),
    ).toBeInTheDocument();
  });

  it("página com histórico resolvido mostra card de incidente resolvido", async () => {
    await renderAt("/status/sp-1");

    expect(await screen.findByText("Indisponibilidade parcial da API")).toBeInTheDocument();
    expect(screen.getByText(/Resolvido \d{2} \w{3}, \d{2}:\d{2}/)).toBeInTheDocument();
  });

  it("status page inexistente ou não publicada mostra página não encontrada", async () => {
    await renderAt("/status/sp-2");

    expect(await screen.findByText("Página não encontrada.")).toBeInTheDocument();
  });

  // UPT-01: exactly 24 hourly bars per service - covered against the
  // generic MSW fixture in test/msw/handlers.ts (already 24-length), not a
  // one-off override, so this also exercises the real fixture-building path.
  it("renderiza exatamente 24 barras horárias por serviço", async () => {
    await renderAt("/status/sp-4");

    expect(await screen.findByText("Fila de processamento")).toBeInTheDocument();
    expect(hourlyBars("Fila de processamento")).toHaveLength(24);
  });

  // UPT-02: each of the four statuses maps to its own bar color.
  it("cada status horário renderiza com a cor correspondente", async () => {
    const now = Date.now();
    const history = Array.from({ length: 24 }, (_, i) =>
      bucket(new Date(now - (23 - i) * 3_600_000).toISOString(), "operational" as PublicHourlyStatus),
    );
    history[5] = bucket(history[5].start, "outage");
    history[10] = bucket(history[10].start, "degraded");
    history[15] = bucket(history[15].start, "no_data");
    mockPublicPreview("Serviço Cores", history);

    await renderAt("/status/hourly-colors-test");

    const bars = await screen.findAllByTestId(/^hourly-bar-Serviço Cores-\d+$/);
    expect(bars).toHaveLength(24);
    expect(bars[0].style.background).toContain("--color-success");
    expect(bars[5].style.background).toContain("--color-critical");
    expect(bars[10].style.background).toContain("--color-warning");
    expect(bars[15].style.background).toContain("--color-neutral-600");
  });

  // UPT-05: hovering/focusing a bar shows the correct local date, hour
  // range, and PT-BR status label, in America/Sao_Paulo regardless of the
  // test runner's own timezone.
  it("cada barra tem tooltip com data, hora e status em português", async () => {
    const history: ReturnType<typeof bucket>[] = Array.from({ length: 24 }, () =>
      bucket("2026-08-24T17:00:00.000Z", "operational" as PublicHourlyStatus),
    );
    history[0] = bucket("2026-08-24T17:00:00.000Z", "degraded");
    mockPublicPreview("Serviço Tooltip", history);

    await renderAt("/status/hourly-tooltip-test");

    const bars = await screen.findAllByTestId(/^hourly-bar-Serviço Tooltip-\d+$/);
    expect(bars[0].title).toBe("24/08, 14h–15h · Degradado");
  });

  // UPT-06: a service with no observed data ever still renders all 24
  // bars as gray no_data, never an empty or missing row.
  it("serviço sem dados renderiza 24 barras cinzas, não uma linha vazia", async () => {
    const history = Array.from({ length: 24 }, () => bucket(new Date().toISOString(), "no_data" as PublicHourlyStatus));
    mockPublicPreview("Serviço Sem Dados", history);

    await renderAt("/status/hourly-no-data-test");

    const bars = await screen.findAllByTestId(/^hourly-bar-Serviço Sem Dados-\d+$/);
    expect(bars).toHaveLength(24);
    for (const bar of bars) {
      expect(bar.style.background).toContain("--color-neutral-600");
    }
  });
});
