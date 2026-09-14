import { describe, it, expect, afterEach, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { server } from "../../test/msw/server";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch } from "../../lib/apiClient";
import { AddServiceDrawer } from "./AddServiceDrawer";

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

afterEach(async () => {
  await apiFetch("/api/auth/logout", { method: "POST" });
});

function renderDrawer(onOpenChange: (open: boolean) => void = () => {}) {
  return render(
    <TestQueryProvider>
      <AddServiceDrawer open onOpenChange={onOpenChange} />
    </TestQueryProvider>
  );
}

describe("AddServiceDrawer", () => {
  it("renderiza nome + busca de SLO e mantém o submit desabilitado até nome e SLO estarem presentes (SVC-23)", async () => {
    await loginAsOwner();
    renderDrawer();

    expect(screen.getByLabelText("Nome do serviço")).toBeInTheDocument();
    expect(screen.getByLabelText("Buscar SLO")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Salvar" })).toBeDisabled();

    await userEvent.type(screen.getByLabelText("Nome do serviço"), "Fila de pagamentos");
    expect(screen.getByRole("button", { name: "Salvar" })).toBeDisabled();

    await userEvent.type(screen.getByLabelText("Buscar SLO"), "checkout");
    const option = await screen.findByRole("button", { name: /Checkout/i });
    await userEvent.click(option);

    expect(screen.getByRole("button", { name: "Salvar" })).toBeEnabled();
  });

  it("submeter com nome e SLO selecionados chama useCreateService com {name, slo_id, slo_name} e fecha o drawer", async () => {
    await loginAsOwner();
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    let closed = false;
    renderDrawer((open) => {
      closed = !open;
    });

    await userEvent.type(screen.getByLabelText("Nome do serviço"), "Fila de pagamentos");
    await userEvent.type(screen.getByLabelText("Buscar SLO"), "checkout");
    const option = await screen.findByRole("button", { name: /Checkout/i });
    await userEvent.click(option);
    await userEvent.click(screen.getByRole("button", { name: "Salvar" }));

    await waitFor(() => expect(closed).toBe(true));

    const call = fetchSpy.mock.calls.find(([url]) => String(url).endsWith("/api/services"));
    expect(call).toBeDefined();
    const body = JSON.parse((call![1] as RequestInit).body as string);
    expect(body).toEqual({ name: "Fila de pagamentos", slo_id: "slo-2", slo_name: "Checkout latência p95" });

    fetchSpy.mockRestore();
  });

  it("submit sem SLO selecionado mostra erro de validação e não fecha o drawer (SVC-23 edge case)", async () => {
    await loginAsOwner();
    let onOpenChangeCalls = 0;
    renderDrawer(() => {
      onOpenChangeCalls += 1;
    });

    // O submit fica desabilitado sem um SLO selecionado (SVC-23); o
    // formulário ainda expõe a validação client-side caso seja submetido
    // por outro meio (ex.: Enter no campo de nome).
    await userEvent.type(screen.getByLabelText("Nome do serviço"), "Serviço sem SLO");
    const form = document.getElementById("add-service-form") as HTMLFormElement;
    form.requestSubmit();

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Selecione um SLO da lista antes de salvar."
    );
    expect(onOpenChangeCalls).toBe(0);
  });

  it("POST /api/services falhando mostra erro inline e não fecha o drawer (SVC-24)", async () => {
    server.use(
      http.post("/api/services", () => HttpResponse.json({ error: "erro interno" }, { status: 500 }))
    );
    await loginAsOwner();
    let onOpenChangeCalls = 0;
    renderDrawer(() => {
      onOpenChangeCalls += 1;
    });

    await userEvent.type(screen.getByLabelText("Nome do serviço"), "Fila de pagamentos");
    await userEvent.type(screen.getByLabelText("Buscar SLO"), "checkout");
    const option = await screen.findByRole("button", { name: /Checkout/i });
    await userEvent.click(option);
    await userEvent.click(screen.getByRole("button", { name: "Salvar" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("erro interno");
    expect(onOpenChangeCalls).toBe(0);
  });

  it("busca de SLO sem resultados mostra 'Nenhum SLO encontrado.'", async () => {
    await loginAsOwner();
    renderDrawer();

    await userEvent.type(screen.getByLabelText("Buscar SLO"), "não existe nenhum slo com esse nome");

    expect(await screen.findByText("Nenhum SLO encontrado.")).toBeInTheDocument();
  });

  it("não renderiza nenhuma opção de Polling manual/New Relic (SVC-25)", async () => {
    await loginAsOwner();
    renderDrawer();

    expect(screen.queryByText(/Polling manual/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/New Relic/i)).not.toBeInTheDocument();
  });
});
