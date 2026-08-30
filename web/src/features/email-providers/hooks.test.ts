import { describe, it, expect } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch, ApiError } from "../../lib/apiClient";
import { useConnectEmailProvider, useActivateEmailProvider, useEmailProviders } from "./hooks";

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

describe("email-providers hooks", () => {
  it("useEmailProviders retorna lista vazia e active_provider nulo quando nada foi conectado", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useEmailProviders(1), { wrapper: TestQueryProvider });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.active_provider).toBeNull();
    expect(result.current.data?.providers).toEqual([]);
  });

  it("useConnectEmailProvider conecta e invalida useEmailProviders em sucesso", async () => {
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

  it("useConnectEmailProvider propaga ApiError 422 numa chave inválida", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useConnectEmailProvider("resend"), { wrapper: TestQueryProvider });

    await expect(
      result.current.mutateAsync({ api_key: "invalid-key", from_email: "a@b.com", from_name: "A" })
    ).rejects.toBeInstanceOf(ApiError);
  });

  it("useActivateEmailProvider ativa um provider conectado e invalida a lista", async () => {
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

  it("useActivateEmailProvider propaga ApiError 422 para um provider não conectado", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useActivateEmailProvider(), { wrapper: TestQueryProvider });

    await expect(result.current.mutateAsync("sendgrid")).rejects.toBeInstanceOf(ApiError);
  });
});
