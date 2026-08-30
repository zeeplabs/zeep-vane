import { describe, it, expect } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch } from "../../lib/apiClient";
import { usePollerStatus } from "./hooks";

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

describe("poller hooks", () => {
  it("usePollerStatus expõe a lista [{provider,status,last_checked_at,last_error}]", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => usePollerStatus(1), { wrapper: TestQueryProvider });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    const entry = result.current.data!.items[0];
    expect(entry).toHaveProperty("provider");
    expect(entry).toHaveProperty("status");
    expect(entry).toHaveProperty("last_checked_at");
    expect(entry).toHaveProperty("last_error");
  });
});
