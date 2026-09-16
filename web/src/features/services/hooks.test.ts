import { describe, it, expect, vi } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch, ApiError } from "../../lib/apiClient";
import { useCreateService, useDeleteService, useServiceDetail, useServices, useUpdateService } from "./hooks";

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

describe("services hooks", () => {
  it("useServices retorna a lista de serviços da fixture", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useServices(1), { wrapper: TestQueryProvider });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data!.items.length).toBeGreaterThan(0);
  });

  // O backend real exige slo_id na criação (services.slo_id NOT NULL,
  // 0004_services.up.sql) - diferente do mock antigo, que permitia criar
  // um serviço sem SLO nenhum. Todo serviço criado nasce "not_configured"
  // até o poller buscar o status pela primeira vez (SPEC_DEVIATION, I15).
  it("serviço criado com slo_id nasce not_configured e resolve slo_name via busca por id", async () => {
    await loginAsOwner();
    const { result } = renderHook(
      () => ({ services: useServices(1), create: useCreateService() }),
      { wrapper: TestQueryProvider }
    );
    await waitFor(() => expect(result.current.services.isSuccess).toBe(true));

    const created = await result.current.create.mutateAsync({
      name: "Serviço com SLO",
      slo_id: "slo-1",
      slo_name: "API disponibilidade 99.9%",
    });
    expect(created.current_status).toBe("not_configured");
    expect(created.slo_name).not.toBeNull();

    await waitFor(() => expect(result.current.services.isFetching).toBe(false));
    const names = result.current.services.data!.items.map((s) => s.name);
    expect(names).toContain("Serviço com SLO");
  });

  it("POST /api/services sem slo_id retorna 422 (mesma regra do backend real)", async () => {
    await loginAsOwner();
    await expect(
      apiFetch("/api/services", {
        method: "POST",
        body: JSON.stringify({ name: "Serviço inválido" }),
      })
    ).rejects.toThrow(ApiError);
  });

  it("useServices(1) usa queryKey com a página e busca /api/services?page=1, retornando o envelope Page completo", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    await loginAsOwner();
    const { result } = renderHook(() => useServices(1), { wrapper: TestQueryProvider });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    const call = fetchSpy.mock.calls.find(([url]) => String(url).includes("/api/services?page=1"));
    expect(call).toBeDefined();

    expect(result.current.data?.page).toBe(1);
    expect(result.current.data?.page_size).toBe(20);
    expect(Array.isArray(result.current.data?.items)).toBe(true);
    expect(typeof result.current.data?.total).toBe("number");

    fetchSpy.mockRestore();
  });

  // SVC-01/SVC-06: useServices must return slo_name/uptime_30d/last_seen_at
  // straight from the single list response, with no per-row live call
  // (the old fetchSLOName call this hook used to make, I15's
  // SPEC_DEVIATION, is gone).
  it("useServices retorna slo_name/uptime_30d/last_seen_at por item com uma única chamada de rede", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    await loginAsOwner();
    const { result } = renderHook(() => useServices(1), { wrapper: TestQueryProvider });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    const items = result.current.data!.items;
    const notConfigured = items.find((s) => s.current_status === "not_configured")!;
    const operational = items.find((s) => s.current_status === "operational")!;
    expect(typeof notConfigured.slo_name).toBe("string");
    expect(notConfigured.uptime_30d).toBeNull();
    expect(notConfigured.last_seen_at).toBeNull();
    expect(operational.uptime_30d).not.toBeNull();
    expect(operational.last_seen_at).not.toBeNull();

    const serviceCalls = fetchSpy.mock.calls.filter(([url]) => String(url).includes("/api/services"));
    expect(serviceCalls).toHaveLength(1);

    fetchSpy.mockRestore();
  });

  // SVC-14..17: useServiceDetail fetches GET /api/services/{id} and returns
  // the flat detail DTO, including all 24 hourly_buckets.
  it("useServiceDetail retorna uptime/incidentes/status_analysis e 24 hourly_buckets", async () => {
    await loginAsOwner();
    const { result: list } = renderHook(() => useServices(1), { wrapper: TestQueryProvider });
    await waitFor(() => expect(list.current.isSuccess).toBe(true));
    const degraded = list.current.data!.items.find((s) => s.current_status === "degraded")!;

    const { result } = renderHook(() => useServiceDetail(degraded.id), { wrapper: TestQueryProvider });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(result.current.data!.id).toBe(degraded.id);
    expect(typeof result.current.data!.incidents_30d).toBe("number");
    expect(result.current.data!.status_analysis).not.toBeNull();
    expect(result.current.data!.hourly_buckets).toHaveLength(24);
  });

  // SVC-20..25: useCreateService sends slo_name in the POST body (the
  // frontend already has it from the selected SLOSummary).
  it("useCreateService envia slo_name no corpo da requisição", async () => {
    await loginAsOwner();
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    const { result } = renderHook(() => useCreateService(), { wrapper: TestQueryProvider });

    await result.current.mutateAsync({ name: "Serviço X", slo_id: "slo-1", slo_name: "SLO da API" });

    const call = fetchSpy.mock.calls.find(([url]) => String(url).endsWith("/api/services"));
    expect(call).toBeDefined();
    const body = JSON.parse((call![1] as RequestInit).body as string);
    expect(body).toEqual({ name: "Serviço X", slo_id: "slo-1", slo_name: "SLO da API" });

    fetchSpy.mockRestore();
  });

  // service-edit SVCEDIT-01/02/06: useUpdateService PATCHes the name and
  // both the list and detail caches reflect it afterward.
  it("useUpdateService renomeia e invalida a lista e o detalhe", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => ({ services: useServices(1), update: useUpdateService() }), {
      wrapper: TestQueryProvider,
    });
    await waitFor(() => expect(result.current.services.isSuccess).toBe(true));
    const target = result.current.services.data!.items[0];

    const updated = await result.current.update.mutateAsync({ id: target.id, name: "Renomeado via teste" });
    expect(updated.name).toBe("Renomeado via teste");

    await waitFor(() => expect(result.current.services.isFetching).toBe(false));
    const names = result.current.services.data!.items.map((s) => s.name);
    expect(names).toContain("Renomeado via teste");
  });

  // service-edit SVCEDIT-03: an empty name rejects with 422, same as Create.
  it("useUpdateService com nome vazio rejeita com 422", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => ({ services: useServices(1), update: useUpdateService() }), {
      wrapper: TestQueryProvider,
    });
    await waitFor(() => expect(result.current.services.isSuccess).toBe(true));
    const target = result.current.services.data!.items[0];

    await expect(result.current.update.mutateAsync({ id: target.id, name: "" })).rejects.toThrow(ApiError);
  });

  // service-delete SVCDEL-01/02/09: useDeleteService removes an unattached
  // service and invalidates the list.
  it("useDeleteService remove um serviço não vinculado e invalida a lista", async () => {
    await loginAsOwner();
    const { result } = renderHook(
      () => ({ services: useServices(1), create: useCreateService(), del: useDeleteService() }),
      { wrapper: TestQueryProvider }
    );
    await waitFor(() => expect(result.current.services.isSuccess).toBe(true));

    const created = await result.current.create.mutateAsync({
      name: "Serviço para excluir",
      slo_id: "slo-delete-hook",
    });

    await result.current.del.mutateAsync(created.id);

    await waitFor(() => expect(result.current.services.isFetching).toBe(false));
    const ids = result.current.services.data!.items.map((s) => s.id);
    expect(ids).not.toContain(created.id);
  });

  // service-delete SVCDEL-03: deleting a service still attached to a
  // status page (fixture svc-1) rejects with ApiError (409).
  it("useDeleteService em serviço vinculado a status page rejeita", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useDeleteService(), { wrapper: TestQueryProvider });

    await expect(result.current.mutateAsync("svc-1")).rejects.toThrow(ApiError);
  });
});
