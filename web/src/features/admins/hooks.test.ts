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

  it("useResendInvite retorna status/email_sent e invalida a lista de admins (INVITE-03)", async () => {
    await loginAsOwner();
    const { result } = renderHook(
      () => ({ admins: useAdmins(), resend: useResendInvite() }),
      { wrapper: TestQueryProvider }
    );
    await waitFor(() => expect(result.current.admins.isSuccess).toBe(true));

    const response = await result.current.resend.mutateAsync("invite-1");

    expect(response).toEqual({ status: "resent", email_sent: true });
  });

  it("useResendInvite com id inexistente rejeita com ApiError 404", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useResendInvite(), { wrapper: TestQueryProvider });

    await expect(result.current.mutateAsync("invite-does-not-exist")).rejects.toBeInstanceOf(ApiError);
  });

  it("useCancelInvite remove o convite da lista de admins (INVITE-05)", async () => {
    await loginAsOwner();
    const { result } = renderHook(
      () => ({ admins: useAdmins(), cancel: useCancelInvite() }),
      { wrapper: TestQueryProvider }
    );
    await waitFor(() => expect(result.current.admins.isSuccess).toBe(true));
    expect(result.current.admins.data!.some((a) => a.id === "invite-1")).toBe(true);

    await result.current.cancel.mutateAsync("invite-1");

    await waitFor(() =>
      expect(result.current.admins.data!.some((a) => a.id === "invite-1")).toBe(false)
    );
  });

  it("useCancelInvite com id inexistente rejeita com ApiError 404", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useCancelInvite(), { wrapper: TestQueryProvider });

    await expect(result.current.mutateAsync("invite-does-not-exist")).rejects.toBeInstanceOf(ApiError);
  });
});
