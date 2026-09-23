import { describe, it, expect, afterEach, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { server } from "../../test/msw/server";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch } from "../../lib/apiClient";
import "../../lib/i18n";
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
  it("renders name + SLO search and keeps submit disabled until name and SLO are present (SVC-23)", async () => {
    await loginAsOwner();
    renderDrawer();

    expect(screen.getByLabelText("Nome do serviço")).toBeInTheDocument();
    expect(screen.getByLabelText("Buscar SLO")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Adicionar serviço" })).toBeDisabled();

    await userEvent.type(screen.getByLabelText("Nome do serviço"), "Fila de pagamentos");
    expect(screen.getByRole("button", { name: "Adicionar serviço" })).toBeDisabled();

    await userEvent.type(screen.getByLabelText("Buscar SLO"), "checkout");
    const option = await screen.findByRole("button", { name: /Checkout/i });
    await userEvent.click(option);

    expect(screen.getByRole("button", { name: "Adicionar serviço" })).toBeEnabled();
  });

  it("submitting with name and SLO selected calls useCreateService with {name, slo_id, slo_name} and closes the drawer", async () => {
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
    await userEvent.click(screen.getByRole("button", { name: "Adicionar serviço" }));

    await waitFor(() => expect(closed).toBe(true));

    const call = fetchSpy.mock.calls.find(([url]) => String(url).endsWith("/api/services"));
    expect(call).toBeDefined();
    const body = JSON.parse((call![1] as RequestInit).body as string);
    expect(body).toEqual({
      name: "Fila de pagamentos",
      slo_id: "slo-2",
      slo_name: "Checkout latência p95",
      slo_type: "metric",
      datadog_service_tag: "checkout-svc",
    });

    fetchSpy.mockRestore();
  });

  // TestAddServiceDrawer covers slo-root-cause-enrichment RCA-01: selecting
  // an SLO captures slo_type/datadog_service_tag from the same search
  // result and includes them in the create payload.
  it("selecting an SLO with a single service tag sends slo_type/datadog_service_tag on save", async () => {
    await loginAsOwner();
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    renderDrawer();

    await userEvent.type(screen.getByLabelText("Nome do serviço"), "API principal");
    await userEvent.type(screen.getByLabelText("Buscar SLO"), "API disponibilidade");
    const option = await screen.findByRole("button", { name: /API disponibilidade/i });
    await userEvent.click(option);
    await userEvent.click(screen.getByRole("button", { name: "Adicionar serviço" }));

    const call = fetchSpy.mock.calls.find(([url]) => String(url).endsWith("/api/services"));
    expect(call).toBeDefined();
    const body = JSON.parse((call![1] as RequestInit).body as string);
    expect(body.slo_type).toBe("metric");
    expect(body.datadog_service_tag).toBe("api-svc");

    fetchSpy.mockRestore();
  });

  // Flow-type SLO (0 or 2+ service_tags entries) has no single resolved
  // service tag - datadog_service_tag must be sent empty, not omitted or
  // invented, same ""-means-absent convention as the rest of this feature.
  it("selecting a flow-type SLO with no single service tag sends an empty datadog_service_tag", async () => {
    await loginAsOwner();
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    renderDrawer();

    await userEvent.type(screen.getByLabelText("Nome do serviço"), "Fila de notificações");
    await userEvent.type(screen.getByLabelText("Buscar SLO"), "Fila de notificações");
    const option = await screen.findByRole("button", { name: /Fila de notificações/i });
    await userEvent.click(option);
    await userEvent.click(screen.getByRole("button", { name: "Adicionar serviço" }));

    const call = fetchSpy.mock.calls.find(([url]) => String(url).endsWith("/api/services"));
    expect(call).toBeDefined();
    const body = JSON.parse((call![1] as RequestInit).body as string);
    expect(body.slo_type).toBe("metric");
    expect(body.datadog_service_tag).toBe("");

    fetchSpy.mockRestore();
  });

  it("submit without an SLO selected shows a validation error and does not close the drawer (SVC-23 edge case)", async () => {
    await loginAsOwner();
    let onOpenChangeCalls = 0;
    renderDrawer(() => {
      onOpenChangeCalls += 1;
    });

    // Submit is disabled without an SLO selected (SVC-23); the form
    // still exposes client-side validation if it is submitted by other
    // means (e.g. Enter in the name field).
    await userEvent.type(screen.getByLabelText("Nome do serviço"), "Serviço sem SLO");
    const form = document.getElementById("add-service-form") as HTMLFormElement;
    form.requestSubmit();

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Selecione um SLO da lista antes de salvar."
    );
    expect(onOpenChangeCalls).toBe(0);
  });

  it("POST /api/services failing shows an inline error and does not close the drawer (SVC-24)", async () => {
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
    await userEvent.click(screen.getByRole("button", { name: "Adicionar serviço" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("erro interno");
    expect(onOpenChangeCalls).toBe(0);
  });

  it("SLO search with no results shows 'Nenhum SLO encontrado.'", async () => {
    await loginAsOwner();
    renderDrawer();

    await userEvent.type(screen.getByLabelText("Buscar SLO"), "não existe nenhum slo com esse nome");

    expect(await screen.findByText("Nenhum SLO encontrado.")).toBeInTheDocument();
  });

  // manual-polling-monitoring T8/P3: the "Como monitorar" toggle defaults to
  // "Baseado em SLO", today's existing behavior/fields unchanged.
  it("defaults to 'Baseado em SLO' with the existing SLO fields visible (P3 AC1)", async () => {
    await loginAsOwner();
    renderDrawer();

    expect(screen.getByRole("button", { name: "Baseado em SLO" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("button", { name: "Polling manual" })).toHaveAttribute("aria-pressed", "false");
    expect(screen.getByLabelText("Buscar SLO")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Datadog" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("button", { name: "New Relic" })).toBeDisabled();
  });

  // P3 AC3: selecting "Polling manual" swaps in check-type/target/interval
  // fields and hides "Fonte"/SLO-search.
  it("selecting 'Polling manual' swaps in check-type/target/interval fields and hides Fonte/SLO-search (P3 AC3)", async () => {
    await loginAsOwner();
    renderDrawer();

    await userEvent.click(screen.getByRole("button", { name: "Polling manual" }));

    expect(screen.queryByLabelText("Buscar SLO")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Datadog" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "HTTP(S)" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "TCP" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Ping" })).toBeInTheDocument();
    expect(screen.getByLabelText("URL a verificar")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "30s" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "1 min" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "5 min" })).toBeInTheDocument();
  });

  // P3 AC3: the target field's label/placeholder switch with the selected
  // check type.
  it("target field label/placeholder switch per check type", async () => {
    await loginAsOwner();
    renderDrawer();
    await userEvent.click(screen.getByRole("button", { name: "Polling manual" }));

    expect(screen.getByLabelText("URL a verificar")).toHaveAttribute("placeholder", "https://api.acme.health/health");

    await userEvent.click(screen.getByRole("button", { name: "TCP" }));
    expect(screen.getByLabelText("Host:porta")).toHaveAttribute("placeholder", "db.acme.health:5432");

    await userEvent.click(screen.getByRole("button", { name: "Ping" }));
    expect(screen.getByLabelText("Host")).toHaveAttribute("placeholder", "cache.acme.health");
  });

  // P3 AC4: clicking the disabled "New Relic" chip does nothing.
  it("clicking the disabled 'New Relic' chip does not change the selected source or submit anything (P3 AC4)", async () => {
    await loginAsOwner();
    renderDrawer();

    const newRelicChip = screen.getByRole("button", { name: "New Relic" });
    expect(newRelicChip).toHaveAttribute("aria-disabled", "true");
    await userEvent.click(newRelicChip);

    expect(screen.getByRole("button", { name: "Datadog" })).toHaveAttribute("aria-pressed", "true");
  });

  // P3 AC5 + T8 "Done when": submit is disabled until the polling mode's
  // required fields are filled, and submitting sends only the polling
  // fields, never slo_id.
  it("polling mode: submit is disabled until target+interval are filled, then sends only the polling fields (never slo_id)", async () => {
    await loginAsOwner();
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    let closed = false;
    renderDrawer((open) => {
      closed = !open;
    });

    await userEvent.type(screen.getByLabelText("Nome do serviço"), "Cache interno");
    await userEvent.click(screen.getByRole("button", { name: "Polling manual" }));
    expect(screen.getByRole("button", { name: "Adicionar serviço" })).toBeDisabled();

    await userEvent.click(screen.getByRole("button", { name: "TCP" }));
    await userEvent.type(screen.getByLabelText("Host:porta"), "cache.acme.health:6379");
    expect(screen.getByRole("button", { name: "Adicionar serviço" })).toBeDisabled();

    await userEvent.click(screen.getByRole("button", { name: "1 min" }));
    expect(screen.getByRole("button", { name: "Adicionar serviço" })).toBeEnabled();

    await userEvent.click(screen.getByRole("button", { name: "Adicionar serviço" }));
    await waitFor(() => expect(closed).toBe(true));

    const call = fetchSpy.mock.calls.find(([url]) => String(url).endsWith("/api/services"));
    expect(call).toBeDefined();
    const body = JSON.parse((call![1] as RequestInit).body as string);
    expect(body).toEqual({
      name: "Cache interno",
      monitor_mode: "polling",
      poll_type: "tcp",
      poll_target: "cache.acme.health:6379",
      poll_interval_seconds: 60,
    });
    expect(body.slo_id).toBeUndefined();

    fetchSpy.mockRestore();
  });

  // T8 "Done when": toggling back to SLO mode keeps the slo-mode submission
  // exactly as before - no poll_* fields leak in.
  it("toggling from polling back to SLO mode still submits only the slo-mode fields (no poll_* leaking in)", async () => {
    await loginAsOwner();
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    renderDrawer();

    await userEvent.click(screen.getByRole("button", { name: "Polling manual" }));
    await userEvent.click(screen.getByRole("button", { name: "TCP" }));
    await userEvent.type(screen.getByLabelText("Host:porta"), "db.acme.health:5432");
    await userEvent.click(screen.getByRole("button", { name: "Baseado em SLO" }));

    await userEvent.type(screen.getByLabelText("Nome do serviço"), "Fila de pagamentos");
    await userEvent.type(screen.getByLabelText("Buscar SLO"), "checkout");
    const option = await screen.findByRole("button", { name: /Checkout/i });
    await userEvent.click(option);
    await userEvent.click(screen.getByRole("button", { name: "Adicionar serviço" }));

    const call = fetchSpy.mock.calls.find(([url]) => String(url).endsWith("/api/services"));
    expect(call).toBeDefined();
    const body = JSON.parse((call![1] as RequestInit).body as string);
    expect(body).toEqual({
      name: "Fila de pagamentos",
      slo_id: "slo-2",
      slo_name: "Checkout latência p95",
      slo_type: "metric",
      datadog_service_tag: "checkout-svc",
    });

    fetchSpy.mockRestore();
  });

  it("blocks 'Baseado em SLO' and defaults to Polling manual when Datadog isn't connected", async () => {
    server.use(
      http.get("/api/integrations/datadog/status", () =>
        HttpResponse.json({ error: "datadog integration not connected yet" }, { status: 404 })
      )
    );
    await loginAsOwner();
    renderDrawer();

    await screen.findByText("Conecte o Datadog em Integrações para usar este modo");
    const sloCard = screen.getByRole("button", { name: "Baseado em SLO" });
    expect(sloCard).toHaveAttribute("aria-disabled", "true");
    // Polling manual's own fields are showing - proof the mode already
    // defaulted there, not left on the now-blocked SLO mode.
    expect(screen.getByLabelText("URL a verificar")).toBeInTheDocument();

    await userEvent.click(sloCard);
    expect(screen.getByLabelText("URL a verificar")).toBeInTheDocument();
  });
});
