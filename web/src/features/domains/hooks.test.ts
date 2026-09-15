import { describe, it, expect, vi } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { server } from "../../test/msw/server";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch, ApiError } from "../../lib/apiClient";
import { useCreateDomain, useDomains, useRecheckDomain } from "./hooks";

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

describe("domains hooks", () => {
  it("useDomains retorna a lista de domínios da fixture", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useDomains(1), { wrapper: TestQueryProvider });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data!.items.length).toBeGreaterThan(0);
  });

  it("useCreateDomain invalida a lista de domínios em sucesso", async () => {
    await loginAsOwner();
    const { result } = renderHook(
      () => ({ domains: useDomains(1), create: useCreateDomain() }),
      { wrapper: TestQueryProvider }
    );
    await waitFor(() => expect(result.current.domains.isSuccess).toBe(true));
    const before = result.current.domains.data!.items.length;

    await result.current.create.mutateAsync({ hostname: "status.novo-teste-hooks.com" });

    await waitFor(() => expect(result.current.domains.data!.items.length).toBe(before + 1));
  });

  it("useDomains(1) usa queryKey com a página e busca /api/domains?page=1, retornando o envelope Page completo", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    await loginAsOwner();
    const { result } = renderHook(() => useDomains(1), { wrapper: TestQueryProvider });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    const call = fetchSpy.mock.calls.find(([url]) => String(url).includes("/api/domains?page=1"));
    expect(call).toBeDefined();

    expect(result.current.data?.page).toBe(1);
    expect(result.current.data?.page_size).toBe(20);
    expect(Array.isArray(result.current.data?.items)).toBe(true);
    expect(typeof result.current.data?.total).toBe("number");

    fetchSpy.mockRestore();
  });

  // domains-status-pages-page T3: the Domain type/response drifted from the
  // real backend (domain_type/status/ssl_status/verified_at/last_error/
  // attached_page_name/attached_page_count) - asserting every new field
  // round-trips guards against that regressing silently again.
  it("useDomains(1) retorna os campos domain_type/status/ssl_status/verified_at/last_error/attached_page_name/attached_page_count e dns_target no envelope", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useDomains(1), { wrapper: TestQueryProvider });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(typeof result.current.data?.dns_target === "string" || result.current.data?.dns_target === null).toBe(true);

    const acme = result.current.data!.items.find((d) => d.hostname === "status.acme.com");
    expect(acme).toBeDefined();
    expect(acme!.domain_type).toBe("custom");
    expect(acme!.status).toBe("verified");
    expect(acme!.ssl_status).toBe("active");
    expect(typeof acme!.verified_at).toBe("string");
    expect(acme!.last_error).toBeNull();
    expect(acme!.attached_page_name).toBe("Status Acme");
    expect(acme!.attached_page_count).toBe(1);

    const beta = result.current.data!.items.find((d) => d.hostname === "status.beta.io");
    expect(beta).toBeDefined();
    expect(beta!.status).toBe("pending");
    expect(beta!.verified_at).toBeNull();
    expect(beta!.attached_page_name).toBeNull();
    expect(beta!.attached_page_count).toBe(0);
  });

  // domains-status-pages-page T4: useRecheckDomain hits POST
  // /api/domains/{id}/verify (DomainsHandler.Verify), distinct from
  // status-pages/hooks.ts's useVerifyDomain (a different endpoint).
  it("useRecheckDomain invalida a lista de domínios em sucesso", async () => {
    await loginAsOwner();
    const { result } = renderHook(
      () => ({ domains: useDomains(1), recheck: useRecheckDomain() }),
      { wrapper: TestQueryProvider }
    );
    await waitFor(() => expect(result.current.domains.isSuccess).toBe(true));
    const target = result.current.domains.data!.items.find((d) => d.hostname === "status.beta.io")!;
    expect(target.status).toBe("pending");

    await result.current.recheck.mutateAsync(target.id);

    await waitFor(() => {
      const updated = result.current.domains.data!.items.find((d) => d.id === target.id);
      expect(updated?.status).toBe("verified");
    });
  });

  it("useRecheckDomain expõe ApiError em caso de erro", async () => {
    await loginAsOwner();
    server.use(
      http.post("/api/domains/:id/verify", () => HttpResponse.json({ error: "internal error" }, { status: 500 }))
    );
    const { result } = renderHook(() => useRecheckDomain(), { wrapper: TestQueryProvider });

    await expect(result.current.mutateAsync("dom-1")).rejects.toBeInstanceOf(ApiError);
  });
});
