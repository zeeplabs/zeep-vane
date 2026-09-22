import { describe, it, expect, afterEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { http, HttpResponse, delay } from "msw";
import i18n from "../../lib/i18n";
import { AuthProvider } from "../../auth/AuthProvider";
import { TestQueryProvider } from "../../test/queryClient";
import { server } from "../../test/msw/server";
import { apiFetch } from "../../lib/apiClient";
import { pollerLeadership, pollerStatus } from "../../lib/mockData";
import { PollerStatusPage } from "./PollerStatusPage";

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

afterEach(async () => {
  pollerStatus[0].status = "active";
  pollerStatus[0].last_error = null;
  pollerLeadership.leader_elected = true;
  pollerLeadership.poller_running = true;
  pollerLeadership.replica = { application_name: "vane-0", backend_start: new Date().toISOString() };
  pollerLeadership.checks_last_minute = 4;
  await apiFetch("/api/auth/logout", { method: "POST" });
});

function renderPage() {
  return render(
    <MemoryRouter>
      <TestQueryProvider>
        <AuthProvider>
          <PollerStatusPage />
        </AuthProvider>
      </TestQueryProvider>
    </MemoryRouter>
  );
}

describe("PollerStatusPage", () => {
  // SKEL-04/05: while /api/poller/status is loading, the page shows
  // skeleton blocks (not the old "Carregando…" paragraph as visible
  // content) inside an aria-busy container that still carries the sr-only
  // loading string.
  it("shows skeletons (not the text) while /api/poller/status is loading", async () => {
    server.use(
      http.get("/api/poller/status", async () => {
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

  // SKEL-06: once the fetch resolves, skeletons are gone and the real
  // stat cards/list take over - loading and loaded are mutually exclusive.
  it("removes the skeletons as soon as /api/poller/status finishes loading", async () => {
    await loginAsOwner();
    renderPage();

    await screen.findByText("Datadog");
    expect(screen.queryAllByTestId("skeleton")).toHaveLength(0);
  });

  it("shows integration, last run and result, error message only on failure", async () => {
    await loginAsOwner();
    renderPage();
    expect(await screen.findByText("Datadog")).toBeInTheDocument();
    expect(screen.getByText("Sucesso")).toBeInTheDocument();
    expect(screen.getAllByText("Última execução").length).toBeGreaterThan(0);
  });

  it("integration with a failure shows the Falha tag and the error message", async () => {
    pollerStatus[0].status = "invalid";
    pollerStatus[0].last_error = "Credenciais inválidas";
    await loginAsOwner();
    renderPage();

    expect(await screen.findByText("Falha")).toBeInTheDocument();
    expect(screen.getByText("Credenciais inválidas")).toBeInTheDocument();
  });

  it("renders Pager with Página 1 de 1 (fixture fits on one page)", async () => {
    await loginAsOwner();
    renderPage();

    await screen.findByText("Datadog");
    expect(screen.getByText("Página 1 de 1")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Anterior" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Próximo" })).toBeDisabled();
  });

  it("clicking Próximo fetches the next page with the correct provider", async () => {
    server.use(
      http.get("/api/poller/status", ({ request }) => {
        const page = new URL(request.url).searchParams.get("page") === "2" ? 2 : 1;
        return HttpResponse.json({
          items: [
            {
              provider: page === 1 ? "datadog" : "sendgrid",
              status: "active",
              last_checked_at: new Date().toISOString(),
              last_error: null,
            },
          ],
          total: 21,
          page,
          page_size: 20,
        });
      })
    );
    await loginAsOwner();
    renderPage();

    await screen.findByText("Datadog");
    await userEvent.click(screen.getByRole("button", { name: "Próximo" }));

    await screen.findByText("SendGrid");
    expect(screen.getByText("Página 2 de 2")).toBeInTheDocument();
  });

  it("active poller shows stat cards with real data (leader, checks/min, integrations)", async () => {
    await loginAsOwner();
    renderPage();

    await screen.findByText("Datadog");
    expect(screen.getByText("Poller")).toBeInTheDocument();
    expect(screen.getByText("Ativo · vane-0")).toBeInTheDocument();
    expect(screen.getByText("Verificações/min")).toBeInTheDocument();
    expect(screen.getByText("4")).toBeInTheDocument();
    expect(screen.getByText("Integrações conectadas")).toBeInTheDocument();
    expect(screen.getByText("1")).toBeInTheDocument();
  });

  it("replica with application_name unknown (HOSTNAME not set) shows local replica", async () => {
    pollerLeadership.replica = { application_name: "unknown", backend_start: new Date().toISOString() };
    await loginAsOwner();
    renderPage();

    expect(await screen.findByText("Ativo · réplica local")).toBeInTheDocument();
  });

  it("checks_last_minute equal to 0 is a valid value, not an error state", async () => {
    pollerLeadership.checks_last_minute = 0;
    await loginAsOwner();
    renderPage();

    await screen.findByText("Verificações/min");
    expect(screen.getByText("0")).toBeInTheDocument();
  });

  it("empty integrations list shows a message and stat cards stay at 0/0", async () => {
    server.use(
      http.get("/api/poller/status", () =>
        HttpResponse.json({
          leader_elected: true,
          poller_running: false,
          replica: { application_name: "vane-0", backend_start: new Date().toISOString() },
          checks_last_minute: 0,
          items: [],
          total: 0,
          page: 1,
          page_size: 20,
        })
      )
    );
    await loginAsOwner();
    renderPage();

    expect(await screen.findByText("Nenhuma integração conectada.")).toBeInTheDocument();
    expect(screen.getByText("Integrações conectadas")).toBeInTheDocument();
    expect(screen.getAllByText("0")).toHaveLength(2);
  });

  it("elected leader without a Datadog integration shows Aguardando integração and an alert banner", async () => {
    pollerLeadership.poller_running = false;
    await loginAsOwner();
    renderPage();

    await screen.findByText("Aguardando integração · vane-0");
    expect(screen.getByText("Réplica líder ativa, mas nenhuma integração Datadog conectada.")).toBeInTheDocument();
  });

  it("no elected leader shows Sem líder no momento, with no replica name", async () => {
    pollerLeadership.leader_elected = false;
    pollerLeadership.poller_running = false;
    pollerLeadership.replica = null;
    await loginAsOwner();
    renderPage();

    await screen.findByText("Sem líder no momento");
  });

  it("failing integration shows an alert banner naming the provider", async () => {
    pollerStatus[0].status = "invalid";
    pollerStatus[0].last_error = "Credenciais inválidas";
    await loginAsOwner();
    renderPage();

    await screen.findByText("Falha");
    expect(
      screen.getByText("Falha ao verificar a integração Datadog — última tentativa não teve sucesso.")
    ).toBeInTheDocument();
  });

  it("leader with no connected integration and a failing integration show both banners simultaneously", async () => {
    pollerLeadership.poller_running = false;
    pollerStatus[0].status = "invalid";
    pollerStatus[0].last_error = "Credenciais inválidas";
    await loginAsOwner();
    renderPage();

    await screen.findByText("Réplica líder ativa, mas nenhuma integração Datadog conectada.");
    expect(
      screen.getByText("Falha ao verificar a integração Datadog — última tentativa não teve sucesso.")
    ).toBeInTheDocument();
  });

  it("healthy state shows no alert banner", async () => {
    await loginAsOwner();
    renderPage();

    await screen.findByText("Datadog");
    expect(
      screen.queryByText("Réplica líder ativa, mas nenhuma integração Datadog conectada.")
    ).not.toBeInTheDocument();
    expect(screen.queryByText(/Falha ao verificar/)).not.toBeInTheDocument();
  });

  it("error loading poller status shows an isolated error state", async () => {
    server.use(http.get("/api/poller/status", () => HttpResponse.json({ error: "boom" }, { status: 500 })));
    await loginAsOwner();
    renderPage();

    expect(await screen.findByText("Não foi possível carregar o status do poller.")).toBeInTheDocument();
  });

  it("renders in English when the active language is en", async () => {
    await loginAsOwner();
    await i18n.changeLanguage("en");

    try {
      renderPage();

      expect(await screen.findByText("Datadog")).toBeInTheDocument();
      expect(screen.getByText("Poller status")).toBeInTheDocument();
      expect(screen.getByText("Checks/min")).toBeInTheDocument();
      expect(screen.getByText("Connected integrations")).toBeInTheDocument();
      expect(screen.getByText("Success")).toBeInTheDocument();
    } finally {
      await i18n.changeLanguage("pt-BR");
    }
  });
});
