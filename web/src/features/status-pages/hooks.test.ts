import { describe, it, expect } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch } from "../../lib/apiClient";
import { useCreateStatusPage, useStatusPages } from "./hooks";

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

describe("status-pages hooks", () => {
  it("useStatusPages retorna a lista da fixture", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useStatusPages(), { wrapper: TestQueryProvider });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data!.length).toBeGreaterThan(0);
  });

  it("useCreateStatusPage invalida a lista e a nova página nasce em draft", async () => {
    await loginAsOwner();
    const { result } = renderHook(
      () => ({ pages: useStatusPages(), create: useCreateStatusPage() }),
      { wrapper: TestQueryProvider }
    );
    await waitFor(() => expect(result.current.pages.isSuccess).toBe(true));

    const created = await result.current.create.mutateAsync({
      name: "Status Hooks Test",
      subdomain: "hookstest",
      domain_id: "dom-1",
      service_ids: [],
    });
    expect(created.state).toBe("draft");

    await waitFor(() =>
      expect(result.current.pages.data!.some((p) => p.id === created.id)).toBe(true)
    );
  });
});
