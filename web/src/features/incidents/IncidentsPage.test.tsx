import { describe, it, expect, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
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

describe("IncidentsPage", () => {
  it("incidente não-resolvido aparece na aba Ativos, separado dos resolvidos", async () => {
    await loginAs("owner@vane.app");
    renderPage();
    expect(await screen.findByText("Latência elevada no Checkout")).toBeInTheDocument();
    expect(screen.queryByText("Indisponibilidade parcial da API")).not.toBeInTheDocument();
  });

  it("aba Resolvidos mostra o incidente resolvido com botão Reabrir para quem gerencia", async () => {
    await loginAs("owner@vane.app");
    renderPage();
    await screen.findByText("Latência elevada no Checkout");
    await userEvent.click(screen.getByRole("tab", { name: "Resolvidos" }));

    expect(await screen.findByText("Indisponibilidade parcial da API")).toBeInTheDocument();
    expect(screen.getByText("Resolvido")).toBeInTheDocument();
    expect(await screen.findByRole("button", { name: /Reabrir incidente/ })).toBeInTheDocument();
  });

  it("viewer não vê o formulário de criação nem o botão de reabrir", async () => {
    await loginAs("viewer@vane.app");
    renderPage();
    await screen.findByText("Latência elevada no Checkout");
    expect(screen.queryByRole("button", { name: "Novo incidente" })).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole("tab", { name: "Resolvidos" }));
    await screen.findByText("Indisponibilidade parcial da API");
    expect(screen.queryByRole("button", { name: /Reabrir incidente/ })).not.toBeInTheDocument();
  });

  it("criar incidente com serviços selecionados aparece na aba Ativos", async () => {
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
  // exercises both the badge-present and badge-absent cases.
  it("incidente auto-criado mostra badge Automático, manual não mostra", async () => {
    await loginAs("owner@vane.app");
    renderPage();

    const activeCard = (await screen.findByText("Latência elevada no Checkout")).closest(
      ".flex.flex-col"
    ) as HTMLElement;
    expect(activeCard).not.toBeNull();
    expect(activeCard.textContent).toContain("Automático");

    await userEvent.click(screen.getByRole("tab", { name: "Resolvidos" }));
    const resolvedCard = (await screen.findByText("Indisponibilidade parcial da API")).closest(
      ".flex.flex-col"
    ) as HTMLElement;
    expect(resolvedCard).not.toBeNull();
    expect(resolvedCard.textContent).not.toContain("Automático");
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
        const body = (await request.json()) as { title: string; service_ids: string[] };
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
});
