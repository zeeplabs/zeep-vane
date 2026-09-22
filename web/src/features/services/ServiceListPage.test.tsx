import { describe, it, expect, afterEach } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse, delay } from "msw";
import { server } from "../../test/msw/server";
import i18n from "../../lib/i18n";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch } from "../../lib/apiClient";
import { ServiceListPage } from "./ServiceListPage";
import type { Page } from "../../types/api";

interface FixtureService {
  id: string;
  name: string;
  slo_id: string | null;
  slo_name: string | null;
  monitor_mode?: "slo" | "polling";
  poll_target?: string | null;
  current_status: "operational" | "degraded" | "outage" | "not_configured";
  last_status_change_at: string;
  uptime_30d: number | null;
  last_seen_at: string | null;
}

// Fixture covering all 4 ServiceStatus values in one page - the shared MSW
// seed (mockData.services) has no "outage" row, and T9's Done-when requires
// all 4 badge labels asserted in one suite (SVC-02..05).
const fourStatusFixture: FixtureService[] = [
  {
    id: "svc-op",
    name: "API Gateway",
    slo_id: "slo-op",
    slo_name: "API disponibilidade",
    current_status: "operational",
    last_status_change_at: new Date().toISOString(),
    uptime_30d: 99.95,
    last_seen_at: new Date().toISOString(),
  },
  {
    id: "svc-deg",
    name: "Checkout",
    slo_id: "slo-deg",
    slo_name: "Checkout p95",
    current_status: "degraded",
    last_status_change_at: new Date().toISOString(),
    uptime_30d: 98.2,
    last_seen_at: new Date().toISOString(),
  },
  {
    id: "svc-down",
    name: "Webhook Receiver",
    slo_id: "slo-down",
    slo_name: "Webhooks disponibilidade",
    current_status: "outage",
    last_status_change_at: new Date().toISOString(),
    uptime_30d: 92.4,
    last_seen_at: new Date().toISOString(),
  },
  {
    id: "svc-nc",
    name: "Notification Queue",
    slo_id: "slo-nc",
    slo_name: "Fila de notificações",
    current_status: "not_configured",
    last_status_change_at: new Date().toISOString(),
    uptime_30d: null,
    last_seen_at: null,
  },
];

function mockServicesPage(items: FixtureService[], total = items.length, pageSize = 20) {
  server.use(
    http.get("/api/services", (): HttpResponse<Page<FixtureService>> => {
      return HttpResponse.json({ items, total, page: 1, page_size: pageSize });
    })
  );
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

function filterChips() {
  return within(screen.getByRole("group", { name: "Filtrar por status" }));
}

function renderPage(
  onSelectService: (id: string) => void = () => {},
  onAddService: () => void = () => {}
) {
  return render(
    <TestQueryProvider>
      <ServiceListPage onSelectService={onSelectService} onAddService={onAddService} />
    </TestQueryProvider>
  );
}

describe("ServiceListPage", () => {
  // SKEL-04/05: while /api/services is loading, skeleton rows matching the
  // real grid template render (not the old "Carregando…" paragraph as
  // visible content) inside an aria-busy container that still carries the
  // sr-only loading string.
  it("mostra skeletons (não o texto) enquanto /api/services carrega", async () => {
    server.use(
      http.get("/api/services", async () => {
        await delay("infinite");
        return HttpResponse.json({ items: [], total: 0, page: 1, page_size: 20 });
      }),
    );
    await loginAsOwner();
    renderPage();

    const srText = await screen.findByText("Carregando…");
    expect(srText.className).toContain("sr-only");
    expect(srText.closest('[aria-busy="true"]')).toBeInTheDocument();
    expect(screen.getAllByTestId("skeleton").length).toBeGreaterThan(0);
  });

  // SKEL-06: once the fetch resolves, skeletons are gone and the real table
  // takes over - loading and loaded are mutually exclusive.
  it("remove os skeletons assim que /api/services termina de carregar", async () => {
    mockServicesPage(fourStatusFixture);
    await loginAsOwner();
    renderPage();

    await screen.findByText("API Gateway");
    expect(screen.queryAllByTestId("skeleton")).toHaveLength(0);
  });

  it("renderiza status/serviço/uptime/última verificação para cada serviço, sem coluna de latência (SVC-01/SVC-07)", async () => {
    mockServicesPage(fourStatusFixture);
    await loginAsOwner();
    renderPage();

    expect(await screen.findByText("API Gateway")).toBeInTheDocument();
    for (const service of fourStatusFixture) {
      expect(screen.getByText(service.name)).toBeInTheDocument();
    }
    expect(screen.queryByText(/Latência/i)).not.toBeInTheDocument();
  });

  it("renderiza o rótulo correto de badge para os 4 valores de CurrentStatus (SVC-02..05)", async () => {
    mockServicesPage(fourStatusFixture);
    await loginAsOwner();
    renderPage();

    await screen.findByText("API Gateway");
    const rows = screen.getAllByTestId("service-row");
    expect(within(rows[0]).getByText("Operacional")).toBeInTheDocument();
    expect(within(rows[1]).getByText("Degradado")).toBeInTheDocument();
    expect(within(rows[2]).getByText("Inativo")).toBeInTheDocument();
    expect(within(rows[3]).getByText("Não configurado")).toBeInTheDocument();
  });

  it("mostra '—' para uptime e última verificação quando o serviço nunca foi verificado (SVC-06)", async () => {
    mockServicesPage(fourStatusFixture);
    await loginAsOwner();
    renderPage();

    await screen.findByText("Notification Queue");
    const notConfiguredRow = screen.getAllByTestId("service-row")[3];
    const dashes = within(notConfiguredRow).getAllByText("—");
    expect(dashes).toHaveLength(2);
  });

  it("filtra pelo chip de status clicado e 'Todos' mostra todos novamente (SVC-09)", async () => {
    mockServicesPage(fourStatusFixture);
    await loginAsOwner();
    renderPage();
    await screen.findByText("API Gateway");

    await userEvent.click(filterChips().getByRole("button", { name: /Degradado/ }));
    expect(screen.queryByText("API Gateway")).not.toBeInTheDocument();
    expect(screen.getByText("Checkout")).toBeInTheDocument();

    await userEvent.click(filterChips().getByRole("button", { name: /Todos/ }));
    expect(screen.getByText("API Gateway")).toBeInTheDocument();
    expect(screen.getByText("Checkout")).toBeInTheDocument();
  });

  it("cada chip mostra a contagem de serviços da página atual (SVC-13)", async () => {
    mockServicesPage(fourStatusFixture);
    await loginAsOwner();
    renderPage();
    await screen.findByText("API Gateway");

    const chips = filterChips();
    expect(chips.getByRole("button", { name: /Todos/ })).toHaveTextContent("4");
    expect(chips.getByRole("button", { name: /Operacional/ })).toHaveTextContent("1");
    expect(chips.getByRole("button", { name: /Degradado/ })).toHaveTextContent("1");
    expect(chips.getByRole("button", { name: /Inativo/ })).toHaveTextContent("1");
    expect(chips.getByRole("button", { name: /Não configurado/ })).toHaveTextContent("1");
  });

  it("busca por nome/SLO, case-insensitive, estreita a lista (SVC-10)", async () => {
    mockServicesPage(fourStatusFixture);
    await loginAsOwner();
    renderPage();
    await screen.findByText("API Gateway");

    await userEvent.type(screen.getByPlaceholderText("Buscar serviço"), "CHECKOUT");
    expect(screen.getByText("Checkout")).toBeInTheDocument();
    expect(screen.queryByText("API Gateway")).not.toBeInTheDocument();
  });

  it("combina filtro de status e busca com AND (SVC-11)", async () => {
    mockServicesPage(fourStatusFixture);
    await loginAsOwner();
    renderPage();
    await screen.findByText("API Gateway");

    await userEvent.click(filterChips().getByRole("button", { name: /Operacional/ }));
    await userEvent.type(screen.getByPlaceholderText("Buscar serviço"), "Checkout");

    expect(screen.queryByText("API Gateway")).not.toBeInTheDocument();
    expect(screen.queryByText("Checkout")).not.toBeInTheDocument();
    expect(screen.getByText("Nenhum serviço encontrado com esses filtros.")).toBeInTheDocument();
  });

  it("mostra o estado vazio quando nenhum serviço casa com filtro/busca (SVC-12)", async () => {
    mockServicesPage(fourStatusFixture);
    await loginAsOwner();
    renderPage();
    await screen.findByText("API Gateway");

    await userEvent.type(screen.getByPlaceholderText("Buscar serviço"), "serviço que não existe");
    expect(screen.getByText("Nenhum serviço encontrado com esses filtros.")).toBeInTheDocument();
  });

  it("renderiza o Pager com totalPages = ceil(total/page_size) quando há mais de uma página (SVC-08)", async () => {
    mockServicesPage(fourStatusFixture, 25, 20);
    await loginAsOwner();
    renderPage();
    await screen.findByText("API Gateway");

    expect(screen.getByText("Página 1 de 2")).toBeInTheDocument();
  });

  it("clicar numa linha chama onSelectService com o id do serviço", async () => {
    mockServicesPage(fourStatusFixture);
    await loginAsOwner();
    let selected: string | null = null;
    renderPage((id) => {
      selected = id;
    });
    await screen.findByText("API Gateway");

    await userEvent.click(screen.getByText("API Gateway"));
    await waitFor(() => expect(selected).toBe("svc-op"));
  });

  // manual-polling-monitoring T9: a polling-manual row (no slo_name/slo_id
  // at all) must show its poll_target as the row subtitle, not blank.
  it("mostra poll_target como subtítulo da linha para um serviço monitor_mode=polling (T9)", async () => {
    mockServicesPage([
      {
        id: "svc-polling",
        name: "Cache interno",
        slo_id: null,
        slo_name: null,
        monitor_mode: "polling",
        poll_target: "cache.acme.health:6379",
        current_status: "operational",
        last_status_change_at: new Date().toISOString(),
        uptime_30d: 99.9,
        last_seen_at: new Date().toISOString(),
      },
    ]);
    await loginAsOwner();
    renderPage();

    await screen.findByText("Cache interno");
    expect(screen.getByText("cache.acme.health:6379")).toBeInTheDocument();
  });

  // Regression: an slo-mode row's subtitle is unchanged.
  it("mantém slo_name como subtítulo da linha para serviços monitor_mode=slo (T9 regressão)", async () => {
    mockServicesPage(fourStatusFixture);
    await loginAsOwner();
    renderPage();

    await screen.findByText("API Gateway");
    expect(screen.getByText("API disponibilidade")).toBeInTheDocument();
    expect(screen.getByText("Checkout p95")).toBeInTheDocument();
  });

  it("clicar em 'Adicionar serviço' chama onAddService", async () => {
    mockServicesPage(fourStatusFixture);
    await loginAsOwner();
    let addClicked = false;
    renderPage(undefined, () => {
      addClicked = true;
    });
    await screen.findByText("API Gateway");

    await userEvent.click(screen.getByRole("button", { name: "Adicionar serviço" }));
    expect(addClicked).toBe(true);
  });

  it("renderiza em inglês quando o idioma ativo é en", async () => {
    mockServicesPage(fourStatusFixture);
    await loginAsOwner();
    await i18n.changeLanguage("en");

    try {
      renderPage();

      expect(await screen.findByText("Monitored services")).toBeInTheDocument();
      expect(screen.getByRole("button", { name: "Add service" })).toBeInTheDocument();
      expect(screen.getByRole("group", { name: "Filter by status" })).toBeInTheDocument();
    } finally {
      await i18n.changeLanguage("pt-BR");
    }
  });
});
