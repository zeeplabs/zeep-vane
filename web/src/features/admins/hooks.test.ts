import { describe, it, expect } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch, ApiError } from "../../lib/apiClient";
import { useAdmins, useDeleteAdmin, useUpdateAdminRole } from "./hooks";

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

describe("admins hooks", () => {
  it("useAdmins reflete status active|pending do backend mock", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useAdmins(), { wrapper: TestQueryProvider });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    const statuses = result.current.data!.map((a) => a.status);
    expect(statuses).toContain("active");
    expect(statuses).toContain("pending");
  });

  it("erro 409 de useUpdateAdminRole (lockout de último owner) propaga a mensagem sem invalidar a lista", async () => {
    await loginAsOwner();
    const { result } = renderHook(
      () => ({ admins: useAdmins(), update: useUpdateAdminRole() }),
      { wrapper: TestQueryProvider }
    );
    await waitFor(() => expect(result.current.admins.isSuccess).toBe(true));
    const before = result.current.admins.data;

    await expect(
      result.current.update.mutateAsync({ id: "admin-1", role: "operator" })
    ).rejects.toBeInstanceOf(ApiError);

    expect(result.current.admins.data).toBe(before);
  });

  it("erro 409 de useDeleteAdmin (lockout de último owner) propaga a mensagem sem invalidar a lista", async () => {
    await loginAsOwner();
    const { result } = renderHook(
      () => ({ admins: useAdmins(), del: useDeleteAdmin() }),
      { wrapper: TestQueryProvider }
    );
    await waitFor(() => expect(result.current.admins.isSuccess).toBe(true));
    const before = result.current.admins.data;

    await expect(result.current.del.mutateAsync("admin-1")).rejects.toBeInstanceOf(ApiError);

    expect(result.current.admins.data).toBe(before);
  });
});
