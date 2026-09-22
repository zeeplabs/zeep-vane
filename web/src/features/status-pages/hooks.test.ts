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
  it("useStatusPages returns the list from the fixture", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useStatusPages(1), { wrapper: TestQueryProvider });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data!.items.length).toBeGreaterThan(0);
  });

  it("useCreateStatusPage sends a body with no domain fields and the new page starts with no domain, in draft (SPD-01)", async () => {
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

  it("useAttachDomain happy path sets domain_id/subdomain and invalidates the list (SPD-06)", async () => {
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

  it("useAttachDomain on a page that already has a domain (sp-1) surfaces as ApiError 409", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useAttachDomain(), { wrapper: TestQueryProvider });

    await expect(
      result.current.mutateAsync({ id: "sp-1", domain_id: "dom-1", subdomain: "outro" })
    ).rejects.toSatisfy((err: unknown) => err instanceof ApiError && err.status === 409);
  });

  it("useAttachDomain with a nonexistent domain_id surfaces as ApiError 422", async () => {
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

  it("useAttachDomain on a nonexistent status page surfaces as ApiError 404", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useAttachDomain(), { wrapper: TestQueryProvider });

    await expect(
      result.current.mutateAsync({ id: "sp-nao-existe", domain_id: "dom-1", subdomain: "novo" })
    ).rejects.toSatisfy((err: unknown) => err instanceof ApiError && err.status === 404);
  });

  it("useDNSTarget returns the configured value (SPD-10)", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useDNSTarget(), { wrapper: TestQueryProvider });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toBe("203.0.113.10");
  });

  it("useDNSTarget returns null when the operator never configured PUBLIC_DNS_TARGET", async () => {
    server.use(
      http.get("/api/instance/dns-target", () => HttpResponse.json({ target: null })),
    );
    await loginAsOwner();
    const { result } = renderHook(() => useDNSTarget(), { wrapper: TestQueryProvider });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toBeNull();
  });
});
