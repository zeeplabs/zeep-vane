import { describe, it, expect } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch } from "../../lib/apiClient";
import { useConnectDatadog, useIntegrationStatus, useSLOSearch } from "./hooks";

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

describe("integrations hooks", () => {
  it("useIntegrationStatus returns the connected status from the fixture", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useIntegrationStatus(), { wrapper: TestQueryProvider });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.connected).toBe(true);
  });

  it("useSLOSearch doesn't fire with an empty query", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useSLOSearch(""), { wrapper: TestQueryProvider });
    expect(result.current.fetchStatus).toBe("idle");
    expect(result.current.data).toBeUndefined();
  });

  it("useSLOSearch returns results filtered by name", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useSLOSearch("checkout"), { wrapper: TestQueryProvider });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.length).toBeGreaterThan(0);
    expect(result.current.data?.[0].name.toLowerCase()).toContain("checkout");
  });

  it("useConnectDatadog invalidates useIntegrationStatus on success", async () => {
    await loginAsOwner();
    const { result } = renderHook(
      () => ({ status: useIntegrationStatus(), connect: useConnectDatadog() }),
      { wrapper: TestQueryProvider }
    );
    await waitFor(() => expect(result.current.status.isSuccess).toBe(true));

    await result.current.connect.mutateAsync({ api_key: "abcd1234wxyz", app_key: "app-key" });

    await waitFor(() => expect(result.current.status.isFetching).toBe(false));
    expect(result.current.connect.isSuccess).toBe(true);
  });
});
