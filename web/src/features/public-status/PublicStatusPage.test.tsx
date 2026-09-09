import { describe, it, expect } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
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
// service carrying an explicit history, so tests can assert exact
// colors/tooltips instead of the generic fixture in test/msw/handlers.ts.
function mockPublicPreview(serviceName: string, history: ReturnType<typeof bucket>[], uptimePercent: number | null = 99.9) {
  server.use(
    http.get("/api/status-pages/:id/public-preview", () =>
      HttpResponse.json({
        company: { name: "Acme Status", logo_url: null },
        services: [
          {
            name: serviceName,
            status: "operational",
            last_updated_at: new Date().toISOString(),
            history,
            uptime_percent: uptimePercent,
          },
        ],
        incidents: { active: [], resolved: { items: [], total: 0, page: 1, page_size: 10 } },
      }),
    ),
  );
}

// mockPublicPreviewMultiService overrides the public-preview MSW handler
// with several services, each getting the same bucket count/status for
// every range tier requested (test double for TRS-03's page-wide
// assertion: a range change must update every service's chart, not just
// one). uptimeByRange lets a test also vary uptime_percent per range
// (TRS-04's frontend-side proof).
function mockPublicPreviewMultiService(
  serviceNames: string[],
  bucketCountByRange: Record<string, number>,
  uptimeByRange?: Record<string, number>,
) {
  server.use(
    http.get("/api/status-pages/:id/public-preview", ({ request }) => {
      const range = new URL(request.url).searchParams.get("range") ?? "24h";
      const bucketCount = bucketCountByRange[range] ?? bucketCountByRange["24h"];
      const now = Date.now();
      const history = Array.from({ length: bucketCount }, (_, i) => ({
        start: new Date(now - (bucketCount - 1 - i) * 3_600_000).toISOString(),
        status: "operational" as PublicHourlyStatus,
      }));
      return HttpResponse.json({
        company: { name: "Acme Status", logo_url: null },
        services: serviceNames.map((name) => ({
          name,
          status: "operational",
          last_updated_at: new Date().toISOString(),
          history,
          uptime_percent: uptimeByRange?.[range] ?? 100,
        })),
        incidents: { active: [], resolved: { items: [], total: 0, page: 1, page_size: 10 } },
      });
    }),
  );
}

function hourlyBars(serviceName: string) {
  return screen.getAllByTestId(new RegExp(`^hourly-bar-${serviceName}-\\d+$`));
}

// mockManyResolvedIncidents overrides the public-preview handler with
// `total` resolved incidents, paginated the same way the real backend does
// (page_size 10, ?page= query param) - so T20's "Carregar mais" can be
// exercised against more than one page without touching the shared fixture.
function mockManyResolvedIncidents(total: number) {
  const all = Array.from({ length: total }, (_, i) => ({
    id: `res-${i + 1}`,
    title: `Resolvido ${i + 1}`,
    status: "resolved" as const,
    created_at: new Date().toISOString(),
    resolved_at: new Date().toISOString(),
    updates: [],
  }));
  server.use(
    http.get("/api/status-pages/:id/public-preview", ({ request }) => {
      const page = Number(new URL(request.url).searchParams.get("page")) || 1;
      const pageSize = 10;
      const start = (page - 1) * pageSize;
      return HttpResponse.json({
        company: { name: "Acme Status", logo_url: null },
        services: [],
        incidents: {
          active: [],
          resolved: { items: all.slice(start, start + pageSize), total: all.length, page, page_size: pageSize },
        },
      });
    }),
  );
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

  // list-pagination T20: resolved incidents load progressively, 10 at a
  // time, via a "Carregar mais" button - never all at once.
  it("mostra 10 incidentes resolvidos e o botão Carregar mais quando há mais de 10", async () => {
    mockManyResolvedIncidents(11);
    await renderAt("/status/resolved-many-test");

    expect(await screen.findByText("Resolvido 1")).toBeInTheDocument();
    expect(screen.getByText("Resolvido 10")).toBeInTheDocument();
    expect(screen.queryByText("Resolvido 11")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Carregar mais" })).toBeInTheDocument();
  });

  it("clicar em Carregar mais adiciona a página seguinte sem duplicar nem reordenar a primeira", async () => {
    mockManyResolvedIncidents(11);
    await renderAt("/status/resolved-many-test");
    await screen.findByText("Resolvido 1");

    await userEvent.click(screen.getByRole("button", { name: "Carregar mais" }));

    expect(await screen.findByText("Resolvido 11")).toBeInTheDocument();
    expect(screen.getByText("Resolvido 1")).toBeInTheDocument();
    expect(screen.getAllByText(/^Resolvido \d+$/)).toHaveLength(11);
  });

  it("botão Carregar mais desaparece quando todos os incidentes resolvidos foram carregados", async () => {
    mockManyResolvedIncidents(11);
    await renderAt("/status/resolved-many-test");
    await screen.findByText("Resolvido 1");

    await userEvent.click(screen.getByRole("button", { name: "Carregar mais" }));
    await screen.findByText("Resolvido 11");

    expect(screen.queryByRole("button", { name: "Carregar mais" })).not.toBeInTheDocument();
  });

  // SEO: this is the one page in the SPA the public internet actually
  // lands on, so it overrides index.html's static admin-product
  // title/description with this company's own status while mounted, and
  // must restore both on unmount - a leftover title from the last status
  // page a visitor viewed must never bleed into whatever they navigate to
  // next in the same tab.
  it("define document.title e a meta description com o nome da empresa, restaurando ambos ao desmontar", async () => {
    const titleBefore = document.title;
    const metaDescription = document.createElement("meta");
    metaDescription.setAttribute("name", "description");
    metaDescription.setAttribute("content", "default admin product description");
    document.head.appendChild(metaDescription);

    try {
      const view = await renderAt("/status/sp-4");
      await screen.findByText("Todos os sistemas operacionais");

      expect(document.title).toBe("Sua Empresa Ltda. Status");
      expect(metaDescription.getAttribute("content")).toBe(
        "Sua Empresa Ltda. — Todos os sistemas operacionais.",
      );

      view.unmount();

      expect(document.title).toBe(titleBefore);
      expect(metaDescription.getAttribute("content")).toBe("default admin product description");
    } finally {
      metaDescription.remove();
    }
  });

  // public-status-time-range-selector T8 / TRS-01: the page loads with 24h
  // selected by default and the existing 24-bar chart, unchanged from today.
  it("carrega com 24h selecionado por padrão e o gráfico de 24 barras existente", async () => {
    await renderAt("/status/sp-4");

    expect(await screen.findByText("Fila de processamento")).toBeInTheDocument();
    expect(hourlyBars("Fila de processamento")).toHaveLength(24);

    const rangeTab24h = screen.getByRole("tab", { name: "24h" });
    expect(rangeTab24h).toHaveAttribute("aria-selected", "true");
    expect(screen.getByText("24h atrás")).toBeInTheDocument();
  });

  // TRS-02/TRS-03: clicking each of the 3 other Seg options triggers a new
  // fetch (asserted via the range value the MSW handler actually received)
  // and updates the leftmost "X atrás" label to match.
  it.each([
    ["7d", "7 dias atrás", 28],
    ["30d", "30 dias atrás", 30],
    ["90d", "90 dias atrás", 90],
  ] as const)("clicar em %s dispara nova busca e atualiza o rótulo para '%s'", async (rangeValue, expectedLabel, expectedBars) => {
    mockPublicPreviewMultiService(["Serviço Range"], { "24h": 24, "7d": 28, "30d": 30, "90d": 90 });
    await renderAt("/status/range-selector-test");

    expect(await screen.findByText("24h atrás")).toBeInTheDocument();
    expect(hourlyBars("Serviço Range")).toHaveLength(24);

    await userEvent.click(screen.getByRole("tab", { name: rangeValue }));

    await waitFor(() => expect(hourlyBars("Serviço Range")).toHaveLength(expectedBars));
    expect(await screen.findByText(expectedLabel)).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: rangeValue })).toHaveAttribute("aria-selected", "true");
  });

  // TRS-03: a range change updates every service's chart page-wide, not
  // just one - asserted with 2 services in the fixture.
  it("mudar o período atualiza o gráfico de todos os serviços, não só um", async () => {
    mockPublicPreviewMultiService(
      ["Serviço A", "Serviço B"],
      { "24h": 24, "7d": 28, "30d": 30, "90d": 90 },
    );
    await renderAt("/status/range-page-wide-test");

    await screen.findByText("Serviço A");
    expect(hourlyBars("Serviço A")).toHaveLength(24);
    expect(hourlyBars("Serviço B")).toHaveLength(24);

    await userEvent.click(screen.getByRole("tab", { name: "90d" }));

    await waitFor(() => expect(hourlyBars("Serviço A")).toHaveLength(90));
    expect(hourlyBars("Serviço B")).toHaveLength(90);
  });

  // TRS-04: the displayed uptime_percent changes when range changes,
  // proving the figure rendered reflects data.services[].uptime_percent per
  // fetch, not a stale cached figure.
  it("uptime_percent exibido muda quando o período muda", async () => {
    mockPublicPreviewMultiService(
      ["Serviço Uptime"],
      { "24h": 24, "90d": 90 },
      { "24h": 99.9, "90d": 95.1 },
    );
    await renderAt("/status/range-uptime-test");

    await screen.findByText("Serviço Uptime");
    expect(await screen.findByTestId("uptime-Serviço Uptime")).toHaveTextContent("99.90% uptime");

    await userEvent.click(screen.getByRole("tab", { name: "90d" }));

    await waitFor(() =>
      expect(screen.getByTestId("uptime-Serviço Uptime")).toHaveTextContent("95.10% uptime"),
    );
  });
});
