import { describe, it, expect, vi } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch } from "../../lib/apiClient";
import { useCreateDomain, useDomains } from "./hooks";

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

describe("domains hooks", () => {
  it("useDomains retorna a lista de domínios da fixture", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useDomains(1), { wrapper: TestQueryProvider });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data!.items.length).toBeGreaterThan(0);
  });

  it("useCreateDomain invalida a lista de domínios em sucesso", async () => {
    await loginAsOwner();
    const { result } = renderHook(
      () => ({ domains: useDomains(1), create: useCreateDomain() }),
      { wrapper: TestQueryProvider }
    );
    await waitFor(() => expect(result.current.domains.isSuccess).toBe(true));
    const before = result.current.domains.data!.items.length;

    await result.current.create.mutateAsync({ hostname: "status.novo-teste-hooks.com" });

    await waitFor(() => expect(result.current.domains.data!.items.length).toBe(before + 1));
  });

  it("useDomains(1) usa queryKey com a página e busca /api/domains?page=1, retornando o envelope Page completo", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    await loginAsOwner();
    const { result } = renderHook(() => useDomains(1), { wrapper: TestQueryProvider });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    const call = fetchSpy.mock.calls.find(([url]) => String(url).includes("/api/domains?page=1"));
    expect(call).toBeDefined();

    expect(result.current.data?.page).toBe(1);
    expect(result.current.data?.page_size).toBe(20);
    expect(Array.isArray(result.current.data?.items)).toBe(true);
    expect(typeof result.current.data?.total).toBe("number");

    fetchSpy.mockRestore();
  });
});
