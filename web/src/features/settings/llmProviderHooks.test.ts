import { describe, it, expect } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch, ApiError } from "../../lib/apiClient";
import {
  useActivateLLMProvider,
  useConnectLLMProvider,
  useDisconnectLLMProvider,
  useLLMProviders,
  useSetLLMProviderModel,
} from "./hooks";
import type { LLMProviderName } from "../../lib/llmProviders";

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

describe("LLM provider hooks", () => {
  it("useLLMProviders returns an empty list and a null active_provider when nothing was connected", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useLLMProviders(1), { wrapper: TestQueryProvider });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.active_provider).toBeNull();
    expect(result.current.data?.providers).toEqual([]);
  });

  it("useConnectLLMProvider connects and invalidates useLLMProviders on success", async () => {
    await loginAsOwner();
    const { result } = renderHook(
      () => ({ list: useLLMProviders(1), connect: useConnectLLMProvider("openai") }),
      { wrapper: TestQueryProvider }
    );
    await waitFor(() => expect(result.current.list.isSuccess).toBe(true));

    await result.current.connect.mutateAsync({ api_key: "sk-real-key" });

    await waitFor(() => expect(result.current.list.isFetching).toBe(false));
    expect(result.current.connect.isSuccess).toBe(true);
    await waitFor(() =>
      expect(result.current.list.data?.providers.some((p) => p.provider === "openai")).toBe(true)
    );
    // No explicit model given, defaults server-side to gpt-4o-mini (AI-03).
    expect(result.current.list.data?.providers.find((p) => p.provider === "openai")?.model).toBe(
      "gpt-4o-mini"
    );
  });

  it("useConnectLLMProvider propagates a 422 ApiError on an invalid key", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useConnectLLMProvider("openai"), { wrapper: TestQueryProvider });

    await expect(result.current.mutateAsync({ api_key: "invalid-key" })).rejects.toBeInstanceOf(ApiError);
  });

  it("useSetLLMProviderModel switches the model of a connected provider without resending the key", async () => {
    await loginAsOwner();
    const { result } = renderHook(
      () => ({
        list: useLLMProviders(1),
        connect: useConnectLLMProvider("openai"),
        setModel: useSetLLMProviderModel("openai"),
      }),
      { wrapper: TestQueryProvider }
    );
    await waitFor(() => expect(result.current.list.isSuccess).toBe(true));
    await result.current.connect.mutateAsync({ api_key: "sk-real-key" });

    await result.current.setModel.mutateAsync("gpt-4o");

    await waitFor(() =>
      expect(result.current.list.data?.providers.find((p) => p.provider === "openai")?.model).toBe("gpt-4o")
    );
  });

  it("useSetLLMProviderModel propagates a 422 ApiError for a model outside the allowlist", async () => {
    await loginAsOwner();
    const { result } = renderHook(
      () => ({ connect: useConnectLLMProvider("openai"), setModel: useSetLLMProviderModel("openai") }),
      { wrapper: TestQueryProvider }
    );
    await result.current.connect.mutateAsync({ api_key: "sk-real-key" });

    await expect(result.current.setModel.mutateAsync("gpt-3.5-turbo")).rejects.toBeInstanceOf(ApiError);
  });

  // MSW-shape-parity regression (AGENTS.md §5): the real backend's
  // Service.SetModel resolves and checks the provider row's connected
  // status before ever looking at the model allowlist (internal/llm/
  // service.go), so a disconnected provider must 422 as "not connected",
  // never "unknown model", even with a bogus model.
  it("useSetLLMProviderModel propagates the 'not connected' error, not 'unknown model', when the provider was never connected", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useSetLLMProviderModel("openai"), { wrapper: TestQueryProvider });

    await expect(result.current.mutateAsync("gpt-3.5-turbo")).rejects.toMatchObject({
      message: expect.stringContaining("not connected"),
    });
  });

  it("useActivateLLMProvider activates a connected provider and invalidates the list", async () => {
    await loginAsOwner();
    const { result } = renderHook(
      () => ({
        list: useLLMProviders(1),
        connect: useConnectLLMProvider("openai"),
        activate: useActivateLLMProvider(),
      }),
      { wrapper: TestQueryProvider }
    );
    await waitFor(() => expect(result.current.list.isSuccess).toBe(true));
    await result.current.connect.mutateAsync({ api_key: "sk-real-key" });

    await result.current.activate.mutateAsync("openai");

    await waitFor(() => expect(result.current.list.data?.active_provider).toBe("openai"));
  });

  it("useActivateLLMProvider propagates a 422 ApiError for an unconnected provider", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useActivateLLMProvider(), { wrapper: TestQueryProvider });

    await expect(result.current.mutateAsync("openai")).rejects.toBeInstanceOf(ApiError);
  });

  // PROVDISC-07/08: mirrors useDisconnectEmailProvider's own coverage - a
  // DELETE with no body, invalidating the same ["integrations", "llm"]
  // query key on success so the disconnected row disappears from the list.
  it("useDisconnectLLMProvider disconnects a connected provider and invalidates the list", async () => {
    await loginAsOwner();
    const { result } = renderHook(
      () => ({
        list: useLLMProviders(1),
        connect: useConnectLLMProvider("openai"),
        disconnect: useDisconnectLLMProvider(),
      }),
      { wrapper: TestQueryProvider }
    );
    await waitFor(() => expect(result.current.list.isSuccess).toBe(true));
    await result.current.connect.mutateAsync({ api_key: "sk-real-key" });
    await waitFor(() =>
      expect(result.current.list.data?.providers.some((p) => p.provider === "openai")).toBe(true)
    );

    await result.current.disconnect.mutateAsync("openai");

    await waitFor(() =>
      expect(result.current.list.data?.providers.some((p) => p.provider === "openai")).toBe(false)
    );
  });

  it("useDisconnectLLMProvider is idempotent - disconnecting a never-connected provider still resolves successfully", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useDisconnectLLMProvider(), { wrapper: TestQueryProvider });

    await expect(result.current.mutateAsync("openai")).resolves.toBeUndefined();
  });

  it("useDisconnectLLMProvider propagates a 404 ApiError for an unknown provider name", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useDisconnectLLMProvider(), { wrapper: TestQueryProvider });

    await expect(result.current.mutateAsync("bogus" as LLMProviderName)).rejects.toBeInstanceOf(ApiError);
  });
});
