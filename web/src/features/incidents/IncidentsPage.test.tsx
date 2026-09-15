import { describe, it, expect, afterEach } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { MemoryRouter } from "react-router-dom";
import "../../lib/i18n";
import { AuthProvider } from "../../auth/AuthProvider";
import { TestQueryProvider } from "../../test/queryClient";
import { server } from "../../test/msw/server";
import { apiFetch } from "../../lib/apiClient";
import { IncidentsPage } from "./IncidentsPage";

async function loginAs(email: string) {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email, password: "demo1234" }),
  });
}

afterEach(async () => {
  await apiFetch("/api/auth/logout", { method: "POST" });
});

function renderPage() {
  return render(
    <MemoryRouter>
      <TestQueryProvider>
        <AuthProvider>
          <IncidentsPage />
        </AuthProvider>
      </TestQueryProvider>
    </MemoryRouter>
  );
}

async function openIncidentRow(title: string) {
  await userEvent.click(await screen.findByText(title));
}

describe("IncidentsPage", () => {
  it("tabela lista incidentes ativos e resolvidos juntos por padrão", async () => {
    await loginAs("owner@vane.app");
    renderPage();
    expect(await screen.findByText("Latência elevada no Checkout")).toBeInTheDocument();
    expect(await screen.findByText("Indisponibilidade parcial da API")).toBeInTheDocument();
  });

  it("chip de status filtra a tabela (Resolvido esconde os ativos)", async () => {
    await loginAs("owner@vane.app");
    renderPage();
    await screen.findByText("Latência elevada no Checkout");

    await userEvent.click(screen.getByRole("button", { name: /Resolvido/ }));

    expect(await screen.findByText("Indisponibilidade parcial da API")).toBeInTheDocument();
    expect(screen.queryByText("Latência elevada no Checkout")).not.toBeInTheDocument();
  });

  it("clicar numa linha abre o drawer de detalhe com título, serviço e severidade", async () => {
    await loginAs("owner@vane.app");
    renderPage();
    await openIncidentRow("Latência elevada no Checkout");

    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByRole("heading", { name: "Latência elevada no Checkout" })).toBeInTheDocument();
    expect(within(dialog).getByTestId("incident-detail-subtitle")).toHaveTextContent("Checkout");
    expect(within(dialog).getByTestId("incident-detail-subtitle")).toHaveTextContent("Crítico");
  });

  it("drawer de incidente resolvido mostra banner e botão Reabrir para quem gerencia", async () => {
    await loginAs("owner@vane.app");
    renderPage();
    await openIncidentRow("Indisponibilidade parcial da API");

    expect(await screen.findByText(/Incidente resolvido em/)).toBeInTheDocument();
    expect(await screen.findByRole("button", { name: /Reabrir incidente/ })).toBeInTheDocument();
  });

  it("viewer não vê 'Novo incidente' nem controles de gestão no drawer", async () => {
    await loginAs("viewer@vane.app");
    renderPage();
    await screen.findByText("Latência elevada no Checkout");
    expect(screen.queryByRole("button", { name: "Novo incidente" })).not.toBeInTheDocument();

    await openIncidentRow("Indisponibilidade parcial da API");
    await screen.findByText(/Incidente resolvido em/);
    expect(screen.queryByRole("button", { name: /Reabrir incidente/ })).not.toBeInTheDocument();
  });

  it("criar incidente com serviços selecionados aparece na tabela", async () => {
    await loginAs("owner@vane.app");
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "Novo incidente" }));
    await userEvent.type(screen.getByLabelText("Título"), "Falha de teste E2E");
    await userEvent.click(screen.getByRole("button", { name: "API pública" }));
    await userEvent.click(screen.getByRole("button", { name: "Criar" }));

    await waitFor(() => expect(screen.queryByLabelText("Título")).not.toBeInTheDocument());
    expect(await screen.findByText("Falha de teste E2E")).toBeInTheDocument();
  });

  // AI-12: inc-1's fixture (mockData.ts) is auto_created:true, inc-2 isn't -
  // exercises both the badge-present and badge-absent cases, in the table
  // row and in the drawer header.
  it("incidente auto-criado mostra badge Automático na linha e no drawer, manual não mostra", async () => {
    await loginAs("owner@vane.app");
    renderPage();

    const activeRow = (await screen.findByText("Latência elevada no Checkout")).closest(
      '[data-testid="incident-row"]'
    ) as HTMLElement;
    expect(activeRow).not.toBeNull();
    expect(activeRow.textContent).toContain("Automático");

    const resolvedRow = (await screen.findByText("Indisponibilidade parcial da API")).closest(
      '[data-testid="incident-row"]'
    ) as HTMLElement;
    expect(resolvedRow).not.toBeNull();
    expect(resolvedRow.textContent).not.toContain("Automático");

    await openIncidentRow("Latência elevada no Checkout");
    const dialog = await screen.findByRole("dialog");
    within(dialog).getByRole("heading", { name: "Latência elevada no Checkout" });
    expect(within(dialog).getByText("Automático")).toBeInTheDocument();
  });

  // PAG-07/PAG-11: the incidents list is paginated (page_size 25). These
  // override the MSW handler with a >25-item set so page 2 exists.
  function paginatedIncidents(items: { id: string; title: string; created_at: string }[]) {
    return http.get("/api/incidents", ({ request }) => {
      const raw = new URL(request.url).searchParams.get("page");
      const page = raw ? Math.max(1, Number.parseInt(raw, 10) || 1) : 1;
      const pageSize = 25;
      return HttpResponse.json({
        items: items.slice((page - 1) * pageSize, (page - 1) * pageSize + pageSize).map((i) => ({
          ...i,
          status: "investigating",
          resolved_at: null,
          service_ids: ["svc-1"],
          description: null,
          pending_close_comment: null,
          auto_created: false,
          severity: "moderate",
        })),
        total: items.length,
        page,
        page_size: pageSize,
      });
    });
  }

  const manyIncidents = Array.from({ length: 30 }, (_, i) => ({
    id: `inc-page-${i + 1}`,
    title: `Incidente paginado ${i + 1}`,
    created_at: new Date(Date.now() - i * 60_000).toISOString(),
  }));

  it("renderiza o Pager e navega para a página seguinte (PAG-07)", async () => {
    server.use(paginatedIncidents(manyIncidents));
    await loginAs("owner@vane.app");
    renderPage();

    expect(await screen.findByText("Incidente paginado 1")).toBeInTheDocument();
    expect(screen.getByText("Página 1 de 2")).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Próximo" }));

    expect(await screen.findByText("Incidente paginado 26")).toBeInTheDocument();
    expect(screen.getByText("Página 2 de 2")).toBeInTheDocument();
    expect(screen.queryByText("Incidente paginado 1")).not.toBeInTheDocument();
  });

  it("criar incidente na página 2 e voltar à página 1 mostra o novo incidente (PAG-11)", async () => {
    const items = [...manyIncidents];
    server.use(
      paginatedIncidents(items),
      http.post("/api/incidents", async ({ request }) => {
        const body = (await request.json()) as { title: string; service_ids: string[]; severity: string };
        const created = {
          id: "inc-created-on-page-2",
          title: body.title,
          created_at: new Date().toISOString(),
        };
        items.unshift(created);
        return HttpResponse.json(
          {
            ...created,
            status: "investigating",
            resolved_at: null,
            service_ids: body.service_ids,
            description: null,
            pending_close_comment: null,
            auto_created: false,
            severity: body.severity,
          },
          { status: 201 }
        );
      })
    );
    await loginAs("owner@vane.app");
    renderPage();

    await screen.findByText("Incidente paginado 1");
    await userEvent.click(screen.getByRole("button", { name: "Próximo" }));
    await screen.findByText("Incidente paginado 26");

    await userEvent.click(screen.getByRole("button", { name: "Novo incidente" }));
    await userEvent.type(screen.getByLabelText("Título"), "Incidente criado na página 2");
    await userEvent.click(screen.getByRole("button", { name: "API pública" }));
    await userEvent.click(screen.getByRole("button", { name: "Criar" }));
    await waitFor(() => expect(screen.queryByLabelText("Título")).not.toBeInTheDocument());

    await userEvent.click(screen.getByRole("button", { name: "Anterior" }));

    expect(await screen.findByText("Incidente criado na página 2")).toBeInTheDocument();
    expect(screen.getByText("Página 1 de 2")).toBeInTheDocument();
  });

  // INCPG-01: inc-1 (mockData.ts) is severity "critical", inc-2 is
  // "moderate" - exercises the badge label mapping in the table.
  it("mostra badge de severidade na tabela (INCPG-01)", async () => {
    await loginAs("owner@vane.app");
    renderPage();

    await screen.findByText("Latência elevada no Checkout");
    expect(screen.getByText("Crítico")).toBeInTheDocument();
    expect(screen.getByText("Moderado")).toBeInTheDocument();
  });

  // INCPG-03/04/05/06/07: create drawer defaults severity to Moderado,
  // sends the selected severity + typed description, and resets both after
  // a successful submit.
  it("criar incidente envia severidade selecionada e descrição, reseta o form (INCPG-03..07)", async () => {
    let capturedBody: { severity?: string; description?: string } | null = null;
    server.use(
      http.post("/api/incidents", async ({ request }) => {
        capturedBody = (await request.json()) as { severity?: string; description?: string };
        return HttpResponse.json(
          {
            id: "inc-severity-test",
            title: "Falha crítica de teste",
            status: "investigating",
            created_at: new Date().toISOString(),
            resolved_at: null,
            service_ids: ["svc-1"],
            description: capturedBody.description ?? null,
            pending_close_comment: null,
            auto_created: false,
            severity: capturedBody.severity,
          },
          { status: 201 }
        );
      })
    );
    await loginAs("owner@vane.app");
    renderPage();

    await userEvent.click(await screen.findByRole("button", { name: "Novo incidente" }));
    expect(screen.getByRole("tab", { name: "Moderado" })).toHaveAttribute("aria-selected", "true");

    await userEvent.type(screen.getByLabelText("Título"), "Falha crítica de teste");
    await userEvent.click(screen.getByRole("button", { name: "API pública" }));
    await userEvent.click(screen.getByRole("tab", { name: "Crítico" }));
    await userEvent.type(screen.getByLabelText("Descrição inicial"), "Impacto total no checkout");
    await userEvent.click(screen.getByRole("button", { name: "Criar" }));

    await waitFor(() => expect(screen.queryByLabelText("Título")).not.toBeInTheDocument());
    expect(capturedBody).toEqual(
      expect.objectContaining({ severity: "critical", description: "Impacto total no checkout" })
    );

    await userEvent.click(screen.getByRole("button", { name: "Novo incidente" }));
    expect(screen.getByRole("tab", { name: "Moderado" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByLabelText("Descrição inicial")).toHaveValue("");
  });

  // INCPG-06: leaving the description field empty does not block submit -
  // the incident is created (real MSW create handler, INCSEV-03's
  // NULL-on-empty convention) with the default severity, and no crash
  // occurs rendering it back in the table/drawer.
  it("criar incidente sem descrição envia o form normalmente (INCPG-06)", async () => {
    await loginAs("owner@vane.app");
    renderPage();

    await userEvent.click(await screen.findByRole("button", { name: "Novo incidente" }));
    await userEvent.type(screen.getByLabelText("Título"), "Incidente sem descrição");
    await userEvent.click(screen.getByRole("button", { name: "API pública" }));
    await userEvent.click(screen.getByRole("button", { name: "Criar" }));

    await waitFor(() => expect(screen.queryByLabelText("Título")).not.toBeInTheDocument());
    await openIncidentRow("Incidente sem descrição");
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByTestId("incident-detail-subtitle")).toHaveTextContent("Moderado");
  });

  // INCPG-08/09/10/11: timeline entries distinguish AI-generated summaries
  // ("Resumo gerado por IA"), human updates ("Equipe"), and system updates
  // with no author ("Sistema"), inside the detail drawer.
  it("timeline no drawer distingue resumo de IA, atualização humana e do sistema (INCPG-09..11)", async () => {
    server.use(
      http.get("/api/incidents/:id/updates", ({ params }) => {
        const incidentId = params.id as string;
        return HttpResponse.json({
          items: [
            {
              id: "upd-ai",
              incident_id: incidentId,
              body: "Resumo automático do incidente.",
              created_at: new Date().toISOString(),
              author_id: null,
              is_ai_summary: true,
            },
            {
              id: "upd-human",
              incident_id: incidentId,
              body: "Investigando a causa raiz.",
              created_at: new Date().toISOString(),
              author_id: "admin-42",
              is_ai_summary: false,
            },
            {
              id: "upd-system",
              incident_id: incidentId,
              body: "Status alterado automaticamente.",
              created_at: new Date().toISOString(),
              author_id: null,
              is_ai_summary: false,
            },
          ],
          total: 3,
          page: 1,
          page_size: 25,
        });
      })
    );
    await loginAs("owner@vane.app");
    renderPage();
    await openIncidentRow("Latência elevada no Checkout");

    expect(await screen.findByText("Resumo gerado por IA")).toBeInTheDocument();
    expect(screen.getAllByText("Equipe").length).toBeGreaterThan(0);
    expect(screen.getByText("Sistema")).toBeInTheDocument();
  });
});
