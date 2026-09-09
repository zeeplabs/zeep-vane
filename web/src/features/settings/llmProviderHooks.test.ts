import { describe, it, expect } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch, ApiError } from "../../lib/apiClient";
import {
  useActivateLLMProvider,
  useConnectLLMProvider,
  useLLMProviders,
  useSetLLMProviderModel,
} from "./hooks";

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

describe("LLM provider hooks", () => {
  it("useLLMProviders retorna lista vazia e active_provider nulo quando nada foi conectado", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useLLMProviders(1), { wrapper: TestQueryProvider });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.active_provider).toBeNull();
    expect(result.current.data?.providers).toEqual([]);
  });

  it("useConnectLLMProvider conecta e invalida useLLMProviders em sucesso", async () => {
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

  it("useConnectLLMProvider propaga ApiError 422 numa chave inválida", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useConnectLLMProvider("openai"), { wrapper: TestQueryProvider });

    await expect(result.current.mutateAsync({ api_key: "invalid-key" })).rejects.toBeInstanceOf(ApiError);
  });

  it("useSetLLMProviderModel troca o modelo de um provider conectado sem reenviar a chave", async () => {
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

  it("useSetLLMProviderModel propaga ApiError 422 para um modelo fora do allowlist", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useSetLLMProviderModel("openai"), { wrapper: TestQueryProvider });

    await expect(result.current.mutateAsync("gpt-3.5-turbo")).rejects.toBeInstanceOf(ApiError);
  });

  it("useActivateLLMProvider ativa um provider conectado e invalida a lista", async () => {
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

  it("useActivateLLMProvider propaga ApiError 422 para um provider não conectado", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useActivateLLMProvider(), { wrapper: TestQueryProvider });

    await expect(result.current.mutateAsync("openai")).rejects.toBeInstanceOf(ApiError);
  });
});
