import { describe, it, expect, vi } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch, ApiError } from "../../lib/apiClient";
import { useCreateService, useServices } from "./hooks";

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
});
