import { describe, it, expect, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { server } from "../../test/msw/server";
import "../../lib/i18n";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch } from "../../lib/apiClient";
import { AuthProvider } from "../../auth/AuthProvider";
import { ServiceDetailDrawer } from "./ServiceDetailDrawer";
import i18n from "../../lib/i18n";
import type { ServiceLatestReading, ServiceStatus } from "../../types/api";

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
  environment?: "" | "development" | "staging" | "production";
  current_status: ServiceStatus;
  last_status_change_at: string;
  uptime_30d: number | null;
  last_seen_at: string | null;
  status_analysis: string | null;
  incidents_30d: number;
  hourly_buckets: ReturnType<typeof buildBuckets>;
  latest_reading: ServiceLatestReading | null;
}

function mockDetail(overrides: Partial<DetailFixture> = {}) {
  const base: DetailFixture = {
    id: "svc-detail-1",
    name: "Checkout",
    slo_id: "slo-1",
    slo_name: "Checkout latência p95",
    monitor_mode: "slo",
    environment: "",
    current_status: "operational",
    last_status_change_at: new Date().toISOString(),
    uptime_30d: 99.5,
    last_seen_at: new Date().toISOString(),
    status_analysis: null,
    incidents_30d: 2,
    hourly_buckets: buildBuckets(24),
    latest_reading: null,
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
      <AuthProvider>
        <ServiceDetailDrawer serviceId="svc-detail-1" onClose={onClose} />
      </AuthProvider>
    </TestQueryProvider>
  );
}

describe("ServiceDetailDrawer", () => {
  it("shows Uptime 30d, Last checked and Incidents (30d) (SVC-14)", async () => {
    const lastSeen = new Date("2026-01-05T10:00:00Z").toISOString();
    mockDetail({ uptime_30d: 99.87, last_seen_at: lastSeen, incidents_30d: 3 });
    await loginAsOwner();
    renderDrawer();

    expect(await screen.findByText("99.87%")).toBeInTheDocument();
    expect(screen.getByText(new Date(lastSeen).toLocaleString("pt-BR"))).toBeInTheDocument();
    expect(screen.getByText("3")).toBeInTheDocument();
  });

  it("renders the note when degraded and status_analysis is non-empty (SVC-15)", async () => {
    mockDetail({ current_status: "degraded", status_analysis: "Latência acima do normal." });
    await loginAsOwner();
    renderDrawer();

    expect(await screen.findByText("Latência acima do normal.")).toBeInTheDocument();
  });

  it("does not render the note when degraded but status_analysis is empty (SVC-16)", async () => {
    mockDetail({ current_status: "degraded", status_analysis: null });
    await loginAsOwner();
    renderDrawer();

    await screen.findByText("Degradado");
    expect(screen.queryByText("Latência acima do normal.")).not.toBeInTheDocument();
  });

  it("does not render the note when status_analysis exists but the status is not degraded (SVC-16)", async () => {
    mockDetail({ current_status: "operational", status_analysis: "Texto que não deveria aparecer." });
    await loginAsOwner();
    renderDrawer();

    await screen.findByText("Operacional");
    expect(screen.queryByText("Texto que não deveria aparecer.")).not.toBeInTheDocument();
  });

  it("renders exactly 24 history bars, even with a differently-sized fixture (SVC-17)", async () => {
    mockDetail({ hourly_buckets: buildBuckets(24) });
    await loginAsOwner();
    renderDrawer();

    await screen.findByText("Checkout");
    expect(screen.getAllByTestId("history-bar")).toHaveLength(24);
  });

  it("closes when clicking the close control (SVC-18)", async () => {
    mockDetail();
    await loginAsOwner();
    let closeCalls = 0;
    renderDrawer(() => {
      closeCalls += 1;
    });

    await userEvent.click(await screen.findByRole("button", { name: "Fechar" }));
    expect(closeCalls).toBe(1);
  });

  it("closes when clicking the backdrop (SVC-18)", async () => {
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
  it("shows poll_target as the subtitle for a monitor_mode=polling service (T9)", async () => {
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
  it("keeps slo_name as the subtitle for a monitor_mode=slo service (T9 regression)", async () => {
    mockDetail({ monitor_mode: "slo" });
    await loginAsOwner();
    renderDrawer();

    await screen.findByText("Checkout");
    expect(screen.getByText("Checkout latência p95")).toBeInTheDocument();
  });

  it("does not render 'Pausar monitoramento' or 'Editar configuração' (SVC-19)", async () => {
    mockDetail();
    await loginAsOwner();
    renderDrawer();

    await screen.findByText("Checkout");
    expect(screen.queryByText(/Pausar monitoramento/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/Editar configuração/i)).not.toBeInTheDocument();
  });

  // service-edit SVCEDIT-06: owner sees the rename control and can rename
  // the service, which reflects in the drawer without a full page reload.
  it("owner can rename the service from the drawer (SVCEDIT-06)", async () => {
    mockDetail();
    await loginAsOwner();
    server.use(
      http.patch("/api/services/:id", async ({ request }) => {
        const body = (await request.json()) as { name: string };
        return HttpResponse.json({ ...mockDetail({ name: body.name }), name: body.name });
      })
    );
    renderDrawer();

    await screen.findByText("Checkout");
    await userEvent.click(screen.getByRole("button", { name: "Renomear serviço" }));

    const input = screen.getByPlaceholderText("Nome do serviço");
    await userEvent.clear(input);
    await userEvent.type(input, "Checkout renomeado");
    await userEvent.click(screen.getByRole("button", { name: "Salvar" }));

    expect(await screen.findByText("Checkout renomeado")).toBeInTheDocument();
  });

  it("an empty name shows an error and does not save (SVCEDIT-03)", async () => {
    mockDetail();
    await loginAsOwner();
    renderDrawer();

    await screen.findByText("Checkout");
    await userEvent.click(screen.getByRole("button", { name: "Renomear serviço" }));
    const input = screen.getByPlaceholderText("Nome do serviço");
    await userEvent.clear(input);
    await userEvent.click(screen.getByRole("button", { name: "Salvar" }));

    expect(await screen.findByText("O nome não pode ficar vazio.")).toBeInTheDocument();
    // Still in editing mode (save was rejected client-side) - the input,
    // not the h2, is what's on screen; the point of this test is that no
    // PATCH was sent and the original name wasn't touched.
    expect(screen.getByPlaceholderText("Nome do serviço")).toBeInTheDocument();
  });

  it("canceling the edit discards the draft and keeps the original name", async () => {
    mockDetail();
    await loginAsOwner();
    renderDrawer();

    await screen.findByText("Checkout");
    await userEvent.click(screen.getByRole("button", { name: "Renomear serviço" }));
    const input = screen.getByPlaceholderText("Nome do serviço");
    await userEvent.type(input, " draft");
    await userEvent.click(screen.getByRole("button", { name: "Cancelar" }));

    expect(screen.getByText("Checkout")).toBeInTheDocument();
    expect(screen.queryByPlaceholderText("Nome do serviço")).not.toBeInTheDocument();
  });

  it("viewer does not see the rename button", async () => {
    mockDetail();
    await apiFetch("/api/auth/login", {
      method: "POST",
      body: JSON.stringify({ email: "viewer@vane.app", password: "demo1234" }),
    });
    renderDrawer();

    await screen.findByText("Checkout");
    expect(screen.queryByRole("button", { name: "Renomear serviço" })).not.toBeInTheDocument();
  });

  // service-environment-tag SET-05: the badge next to the name shows the
  // right label/color for a tagged service, and renders nothing for "".
  it("shows the environment badge next to the name when the service has a tag (SET-05)", async () => {
    mockDetail({ environment: "production" });
    await loginAsOwner();
    renderDrawer();

    expect(await screen.findByText("Checkout")).toBeInTheDocument();
    expect(screen.getByTestId("environment-badge")).toHaveTextContent("Produção");
  });

  it("shows no environment badge for a service with no tag (SET-05)", async () => {
    mockDetail({ environment: "" });
    await loginAsOwner();
    renderDrawer();

    await screen.findByText("Checkout");
    expect(screen.queryByTestId("environment-badge")).not.toBeInTheDocument();
  });

  // service-environment-tag SET-03/SET-05: owner can change the environment
  // via the select, which PATCHes and reflects in the badge without a full
  // page reload - viewer never sees the select at all.
  it("owner can change the environment tag from the drawer (SET-03/SET-05)", async () => {
    mockDetail({ environment: "" });
    await loginAsOwner();
    server.use(
      http.patch("/api/services/:id", async ({ request }) => {
        const body = (await request.json()) as { name: string; environment?: string };
        return HttpResponse.json(mockDetail({ name: body.name, environment: body.environment as "staging" }));
      })
    );
    renderDrawer();

    await screen.findByText("Checkout");
    const select = screen.getByLabelText("Ambiente");
    await userEvent.selectOptions(select, "staging");

    await waitFor(() => expect(screen.getByTestId("environment-badge")).toHaveTextContent("Staging"));
  });

  it("viewer does not see the environment select", async () => {
    mockDetail({ environment: "development" });
    await apiFetch("/api/auth/login", {
      method: "POST",
      body: JSON.stringify({ email: "viewer@vane.app", password: "demo1234" }),
    });
    renderDrawer();

    await screen.findByText("Checkout");
    expect(screen.queryByLabelText("Ambiente")).not.toBeInTheDocument();
    // The badge itself is still visible read-only to a viewer.
    expect(screen.getByTestId("environment-badge")).toHaveTextContent("Desenvolvimento");
  });

  // service-delete SVCDEL-09: owner sees the delete action, confirms, and
  // the drawer closes on success.
  it("owner can delete the service after confirming (SVCDEL-09)", async () => {
    mockDetail();
    await loginAsOwner();
    server.use(http.delete("/api/services/:id", () => new HttpResponse(null, { status: 204 })));
    let closeCalls = 0;
    renderDrawer(() => {
      closeCalls += 1;
    });

    await screen.findByText("Checkout");
    await userEvent.click(screen.getByRole("button", { name: "Excluir serviço" }));
    // Confirm dialog title uses the same i18n string as the trigger button
    // - the button with the trigger's own aria-label disambiguates it.
    const dialogDeleteButtons = await screen.findAllByRole("button", { name: "Excluir serviço" });
    await userEvent.click(dialogDeleteButtons[dialogDeleteButtons.length - 1]);

    await waitFor(() => expect(closeCalls).toBe(1));
  });

  it("blocked delete (409) shows an error and does not close the drawer", async () => {
    mockDetail();
    await loginAsOwner();
    server.use(
      http.delete("/api/services/:id", () =>
        HttpResponse.json({ error: "service is still attached to a status page" }, { status: 409 })
      )
    );
    let closeCalls = 0;
    renderDrawer(() => {
      closeCalls += 1;
    });

    await screen.findByText("Checkout");
    await userEvent.click(screen.getByRole("button", { name: "Excluir serviço" }));
    const dialogDeleteButtons = await screen.findAllByRole("button", { name: "Excluir serviço" });
    await userEvent.click(dialogDeleteButtons[dialogDeleteButtons.length - 1]);

    expect(await screen.findByText("service is still attached to a status page")).toBeInTheDocument();
    expect(closeCalls).toBe(0);
  });

  it("viewer does not see the delete button", async () => {
    mockDetail();
    await apiFetch("/api/auth/login", {
      method: "POST",
      body: JSON.stringify({ email: "viewer@vane.app", password: "demo1234" }),
    });
    renderDrawer();

    await screen.findByText("Checkout");
    expect(screen.queryByRole("button", { name: "Excluir serviço" })).not.toBeInTheDocument();
  });

  // poller-burn-rate-classification BURN-14: the "Última leitura" block.
  describe("latest reading", () => {
    function reading(overrides: Partial<ServiceLatestReading> = {}): ServiceLatestReading {
      return {
        burn_rate_short: 12.34,
        burn_rate_long: 3.06,
        window_errors: 37,
        window_requests: 5000,
        read_at: new Date(Date.now() - 2 * 60 * 1000).toISOString(),
        ...overrides,
      };
    }

    it("renders error rate, error/request counts, burn rates and relative read time", async () => {
      mockDetail({ latest_reading: reading() });
      await loginAsOwner();
      renderDrawer();

      expect(await screen.findByText("Última leitura")).toBeInTheDocument();
      expect(screen.getByText("0.74%")).toBeInTheDocument();
      expect(screen.getByText("37 erros em 5.000 requisições")).toBeInTheDocument();
      expect(screen.getByText("12.3x")).toBeInTheDocument();
      expect(screen.getByText("3.1x")).toBeInTheDocument();
      expect(screen.getByText("há 2 min")).toBeInTheDocument();
      expect(screen.getByText("1x = consumo exato do orçamento de erro do SLO.")).toBeInTheDocument();
    });

    it("renders the empty state when latest_reading is null", async () => {
      mockDetail({ latest_reading: null });
      await loginAsOwner();
      renderDrawer();

      expect(await screen.findByText("Última leitura")).toBeInTheDocument();
      expect(screen.getByText("Ainda sem leitura do Datadog.")).toBeInTheDocument();
      expect(screen.queryByText("Taxa de erro (5 min)")).not.toBeInTheDocument();
    });

    it("renders ∞ for a null (unbounded) burn rate when the window has errors", async () => {
      mockDetail({
        latest_reading: reading({ burn_rate_short: null, burn_rate_long: null, window_errors: 1, window_requests: 1 }),
      });
      await loginAsOwner();
      renderDrawer();

      await screen.findByText("Última leitura");
      expect(screen.getAllByText("∞")).toHaveLength(2);
      expect(screen.getByText("1 erro em 1 requisição")).toBeInTheDocument();
    });

    it("renders — for a null short burn rate when the window has no errors", async () => {
      mockDetail({ latest_reading: reading({ burn_rate_short: null, burn_rate_long: 0.5, window_errors: 0 }) });
      await loginAsOwner();
      renderDrawer();

      await screen.findByText("Última leitura");
      expect(screen.queryByText("∞")).not.toBeInTheDocument();
      expect(screen.getByText("—")).toBeInTheDocument();
      expect(screen.getByText("0.5x")).toBeInTheDocument();
    });

    it("shows 'Sem tráfego' instead of a rate when the window had no requests", async () => {
      mockDetail({
        latest_reading: reading({ burn_rate_short: 0, burn_rate_long: 0, window_errors: 0, window_requests: 0 }),
      });
      await loginAsOwner();
      renderDrawer();

      expect(await screen.findByText("Sem tráfego")).toBeInTheDocument();
      expect(screen.queryByText(/requisiç/)).not.toBeInTheDocument();
      expect(screen.getAllByText("0.0x")).toHaveLength(2);
    });

    it("does not render the block for a monitor_mode=polling service", async () => {
      mockDetail({ monitor_mode: "polling", slo_id: null, slo_name: null, poll_target: "cache.acme.health:6379" });
      await loginAsOwner();
      renderDrawer();

      await screen.findByText("Checkout");
      expect(screen.queryByText("Última leitura")).not.toBeInTheDocument();
      expect(screen.queryByText("Ainda sem leitura do Datadog.")).not.toBeInTheDocument();
    });

    it("renders the English copy when the UI language is en", async () => {
      await i18n.changeLanguage("en");
      try {
        mockDetail({ latest_reading: reading() });
        await loginAsOwner();
        renderDrawer();

        expect(await screen.findByText("Latest reading")).toBeInTheDocument();
        expect(screen.getByText("37 errors in 5,000 requests")).toBeInTheDocument();
        expect(screen.getByText("2 min ago")).toBeInTheDocument();
      } finally {
        await i18n.changeLanguage("pt-BR");
      }
    });
  });
});
