import { describe, it, expect } from "vitest";
import { http, HttpResponse } from "msw";
import { renderHook, waitFor } from "@testing-library/react";
import { server } from "../../test/msw/server";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch, ApiError } from "../../lib/apiClient";
import { useAttachDomain, useCreateStatusPage, useDNSTarget, useStatusPages } from "./hooks";

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

describe("status-pages hooks", () => {
  it("useStatusPages retorna a lista da fixture", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useStatusPages(1), { wrapper: TestQueryProvider });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data!.items.length).toBeGreaterThan(0);
  });

  it("useCreateStatusPage envia corpo sem campos de domínio e a nova página nasce sem domínio, em draft (SPD-01)", async () => {
    await loginAsOwner();
    const { result } = renderHook(
      () => ({ pages: useStatusPages(1), create: useCreateStatusPage() }),
      { wrapper: TestQueryProvider }
    );
    await waitFor(() => expect(result.current.pages.isSuccess).toBe(true));

    const created = await result.current.create.mutateAsync({
      name: "Status Hooks Test",
      service_ids: [],
    });
    expect(created.state).toBe("draft");
    expect(created.domain_id).toBeNull();
    expect(created.subdomain).toBeNull();

    await waitFor(() =>
      expect(result.current.pages.data!.items.some((p) => p.id === created.id)).toBe(true)
    );
  });

  it("useAttachDomain caminho feliz seta domain_id/subdomain e invalida a lista (SPD-06)", async () => {
    await loginAsOwner();
    const { result } = renderHook(
      () => ({ pages: useStatusPages(1), create: useCreateStatusPage(), attach: useAttachDomain() }),
      { wrapper: TestQueryProvider }
    );
    await waitFor(() => expect(result.current.pages.isSuccess).toBe(true));

    const domainless = await result.current.create.mutateAsync({
      name: "Página sem domínio",
      service_ids: [],
    });

    const attached = await result.current.attach.mutateAsync({
      id: domainless.id,
      domain_id: "dom-1",
      subdomain: "attached-hooks-test",
    });
    expect(attached.domain_id).toBe("dom-1");
    expect(attached.subdomain).toBe("attached-hooks-test");

    await waitFor(() =>
      expect(result.current.pages.data!.items.find((p) => p.id === domainless.id)?.domain_id).toBe("dom-1")
    );
  });

  it("useAttachDomain em página já com domínio (sp-1) surge como ApiError 409", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useAttachDomain(), { wrapper: TestQueryProvider });

    await expect(
      result.current.mutateAsync({ id: "sp-1", domain_id: "dom-1", subdomain: "outro" })
    ).rejects.toSatisfy((err: unknown) => err instanceof ApiError && err.status === 409);
  });

  it("useAttachDomain com domain_id inexistente surge como ApiError 422", async () => {
    await loginAsOwner();
    const { result } = renderHook(
      () => ({ create: useCreateStatusPage(), attach: useAttachDomain() }),
      { wrapper: TestQueryProvider }
    );
    const domainless = await result.current.create.mutateAsync({
      name: "Página sem domínio 2",
      service_ids: [],
    });

    await expect(
      result.current.attach.mutateAsync({
        id: domainless.id,
        domain_id: "dom-inexistente",
        subdomain: "novo",
      })
    ).rejects.toSatisfy((err: unknown) => err instanceof ApiError && err.status === 422);
  });

  it("useAttachDomain em status page inexistente surge como ApiError 404", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useAttachDomain(), { wrapper: TestQueryProvider });

    await expect(
      result.current.mutateAsync({ id: "sp-nao-existe", domain_id: "dom-1", subdomain: "novo" })
    ).rejects.toSatisfy((err: unknown) => err instanceof ApiError && err.status === 404);
  });

  it("useDNSTarget retorna o valor configurado (SPD-10)", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useDNSTarget(), { wrapper: TestQueryProvider });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toBe("203.0.113.10");
  });

  it("useDNSTarget retorna null quando o operador nunca configurou PUBLIC_DNS_TARGET", async () => {
    server.use(
      http.get("/api/instance/dns-target", () => HttpResponse.json({ target: null })),
    );
    await loginAsOwner();
    const { result } = renderHook(() => useDNSTarget(), { wrapper: TestQueryProvider });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toBeNull();
  });
});
