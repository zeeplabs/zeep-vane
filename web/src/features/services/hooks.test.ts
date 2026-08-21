import { describe, it, expect } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch } from "../../lib/apiClient";
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
    const { result } = renderHook(() => useServices(), { wrapper: TestQueryProvider });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data!.length).toBeGreaterThan(0);
  });

  it("serviço sem slo_id fica not_configured; com slo_id vinculado remove o rótulo", async () => {
    await loginAsOwner();
    const { result } = renderHook(
      () => ({ services: useServices(), create: useCreateService() }),
      { wrapper: TestQueryProvider }
    );
    await waitFor(() => expect(result.current.services.isSuccess).toBe(true));

    const withoutSlo = await result.current.create.mutateAsync({ name: "Serviço sem SLO" });
    expect(withoutSlo.current_status).toBe("not_configured");
    expect(withoutSlo.slo_name).toBeNull();

    const withSlo = await result.current.create.mutateAsync({
      name: "Serviço com SLO",
      slo_id: "slo-1",
    });
    expect(withSlo.slo_name).not.toBeNull();
    expect(withSlo.current_status).not.toBe("not_configured");

    await waitFor(() => expect(result.current.services.isFetching).toBe(false));
    const names = result.current.services.data!.map((s) => s.name);
    expect(names).toContain("Serviço sem SLO");
    expect(names).toContain("Serviço com SLO");
  });
});
