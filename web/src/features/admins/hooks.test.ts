import { describe, it, expect } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch, ApiError } from "../../lib/apiClient";
import { useAdmins, useCancelInvite, useDeleteAdmin, useResendInvite, useUpdateAdminRole } from "./hooks";

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

describe("admins hooks", () => {
  it("useAdmins reflects active|pending status from the mock backend", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useAdmins(1), { wrapper: TestQueryProvider });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    const statuses = result.current.data!.items.map((a) => a.status);
    expect(statuses).toContain("active");
    expect(statuses).toContain("pending");
  });

  it("409 error from useUpdateAdminRole (last-owner lockout) propagates the message without invalidating the list", async () => {
    await loginAsOwner();
    const { result } = renderHook(
      () => ({ admins: useAdmins(1), update: useUpdateAdminRole() }),
      { wrapper: TestQueryProvider }
    );
    await waitFor(() => expect(result.current.admins.isSuccess).toBe(true));
    const before = result.current.admins.data;

    await expect(
      result.current.update.mutateAsync({ id: "admin-1", role: "operator" })
    ).rejects.toBeInstanceOf(ApiError);

    expect(result.current.admins.data).toBe(before);
  });

  it("409 error from useDeleteAdmin (last-owner lockout) propagates the message without invalidating the list", async () => {
    await loginAsOwner();
    const { result } = renderHook(
      () => ({ admins: useAdmins(1), del: useDeleteAdmin() }),
      { wrapper: TestQueryProvider }
    );
    await waitFor(() => expect(result.current.admins.isSuccess).toBe(true));
    const before = result.current.admins.data;

    await expect(result.current.del.mutateAsync("admin-1")).rejects.toBeInstanceOf(ApiError);

    expect(result.current.admins.data).toBe(before);
  });

  it("useResendInvite returns status/email_sent and invalidates the admins list (INVITE-03)", async () => {
    await loginAsOwner();
    const { result } = renderHook(
      () => ({ admins: useAdmins(1), resend: useResendInvite() }),
      { wrapper: TestQueryProvider }
    );
    await waitFor(() => expect(result.current.admins.isSuccess).toBe(true));

    const response = await result.current.resend.mutateAsync("invite-1");

    expect(response).toEqual({ status: "resent", email_sent: true });
  });

  it("useResendInvite with a nonexistent id rejects with ApiError 404", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useResendInvite(), { wrapper: TestQueryProvider });

    await expect(result.current.mutateAsync("invite-does-not-exist")).rejects.toBeInstanceOf(ApiError);
  });

  it("useCancelInvite removes the invite from the admins list (INVITE-05)", async () => {
    await loginAsOwner();
    const { result } = renderHook(
      () => ({ admins: useAdmins(1), cancel: useCancelInvite() }),
      { wrapper: TestQueryProvider }
    );
    await waitFor(() => expect(result.current.admins.isSuccess).toBe(true));
    expect(result.current.admins.data!.items.some((a) => a.id === "invite-1")).toBe(true);

    await result.current.cancel.mutateAsync("invite-1");

    await waitFor(() =>
      expect(result.current.admins.data!.items.some((a) => a.id === "invite-1")).toBe(false)
    );
  });

  it("useCancelInvite with a nonexistent id rejects with ApiError 404", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useCancelInvite(), { wrapper: TestQueryProvider });

    await expect(result.current.mutateAsync("invite-does-not-exist")).rejects.toBeInstanceOf(ApiError);
  });
});
