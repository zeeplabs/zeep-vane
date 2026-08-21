import { describe, it, expect } from "vitest";
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
    const { result } = renderHook(() => useDomains(), { wrapper: TestQueryProvider });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data!.length).toBeGreaterThan(0);
  });

  it("useCreateDomain invalida a lista de domínios em sucesso", async () => {
    await loginAsOwner();
    const { result } = renderHook(
      () => ({ domains: useDomains(), create: useCreateDomain() }),
      { wrapper: TestQueryProvider }
    );
    await waitFor(() => expect(result.current.domains.isSuccess).toBe(true));
    const before = result.current.domains.data!.length;

    await result.current.create.mutateAsync({ hostname: "status.novo-teste-hooks.com" });

    await waitFor(() => expect(result.current.domains.data!.length).toBe(before + 1));
  });
});
