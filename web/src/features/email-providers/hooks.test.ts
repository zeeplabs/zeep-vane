import { describe, it, expect } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch, ApiError } from "../../lib/apiClient";
import {
  useConnectEmailProvider,
  useActivateEmailProvider,
  useDisconnectEmailProvider,
  useEmailProviders,
  type EmailProviderName,
} from "./hooks";

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

describe("email-providers hooks", () => {
  it("useEmailProviders returns an empty list and a null active_provider when nothing was connected", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useEmailProviders(1), { wrapper: TestQueryProvider });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.active_provider).toBeNull();
    expect(result.current.data?.providers).toEqual([]);
  });

  it("useConnectEmailProvider connects and invalidates useEmailProviders on success", async () => {
    await loginAsOwner();
    const { result } = renderHook(
      () => ({ list: useEmailProviders(1), connect: useConnectEmailProvider("sendgrid") }),
      { wrapper: TestQueryProvider }
    );
    await waitFor(() => expect(result.current.list.isSuccess).toBe(true));

    await result.current.connect.mutateAsync({
      api_key: "sg-real-key",
      from_email: "noreply@acme.example.com",
      from_name: "Acme",
    });

    await waitFor(() => expect(result.current.list.isFetching).toBe(false));
    expect(result.current.connect.isSuccess).toBe(true);
    await waitFor(() =>
      expect(result.current.list.data?.providers.some((p) => p.provider === "sendgrid")).toBe(true)
    );
  });

  it("useConnectEmailProvider propagates ApiError 422 for an invalid key", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useConnectEmailProvider("resend"), { wrapper: TestQueryProvider });

    await expect(
      result.current.mutateAsync({ api_key: "invalid-key", from_email: "a@b.com", from_name: "A" })
    ).rejects.toBeInstanceOf(ApiError);
  });

  it("useActivateEmailProvider activates a connected provider and invalidates the list", async () => {
    await loginAsOwner();
    const { result } = renderHook(
      () => ({
        list: useEmailProviders(1),
        connect: useConnectEmailProvider("resend"),
        activate: useActivateEmailProvider(),
      }),
      { wrapper: TestQueryProvider }
    );
    await waitFor(() => expect(result.current.list.isSuccess).toBe(true));
    await result.current.connect.mutateAsync({
      api_key: "re-real-key",
      from_email: "noreply@acme.example.com",
      from_name: "Acme",
    });

    await result.current.activate.mutateAsync("resend");

    await waitFor(() => expect(result.current.list.data?.active_provider).toBe("resend"));
  });

  it("useActivateEmailProvider propagates ApiError 422 for a provider that isn't connected", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useActivateEmailProvider(), { wrapper: TestQueryProvider });

    await expect(result.current.mutateAsync("sendgrid")).rejects.toBeInstanceOf(ApiError);
  });

  // PROVDISC-07/08: mirrors useActivateEmailProvider's own coverage - a
  // DELETE with no body, invalidating the same ["integrations", "email"]
  // query key on success so the disconnected row disappears from the list.
  it("useDisconnectEmailProvider disconnects a connected provider and invalidates the list", async () => {
    await loginAsOwner();
    const { result } = renderHook(
      () => ({
        list: useEmailProviders(1),
        connect: useConnectEmailProvider("sendgrid"),
        disconnect: useDisconnectEmailProvider(),
      }),
      { wrapper: TestQueryProvider }
    );
    await waitFor(() => expect(result.current.list.isSuccess).toBe(true));
    await result.current.connect.mutateAsync({
      api_key: "sg-real-key",
      from_email: "noreply@acme.example.com",
      from_name: "Acme",
    });
    await waitFor(() =>
      expect(result.current.list.data?.providers.some((p) => p.provider === "sendgrid")).toBe(true)
    );

    await result.current.disconnect.mutateAsync("sendgrid");

    await waitFor(() =>
      expect(result.current.list.data?.providers.some((p) => p.provider === "sendgrid")).toBe(false)
    );
  });

  it("useDisconnectEmailProvider is idempotent - disconnecting a provider that was never connected still resolves successfully", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useDisconnectEmailProvider(), { wrapper: TestQueryProvider });

    await expect(result.current.mutateAsync("resend")).resolves.toBeUndefined();
  });

  it("useDisconnectEmailProvider propagates ApiError 404 for an unknown provider name", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useDisconnectEmailProvider(), { wrapper: TestQueryProvider });

    await expect(
      result.current.mutateAsync("mailgun" as EmailProviderName)
    ).rejects.toBeInstanceOf(ApiError);
  });
});
