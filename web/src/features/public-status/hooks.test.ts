import { describe, it, expect, afterEach } from "vitest";
import { http, HttpResponse } from "msw";
import { act, renderHook, waitFor } from "@testing-library/react";
import { server } from "../../test/msw/server";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch } from "../../lib/apiClient";
import { usePublicStatusPage } from "./hooks";

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

afterEach(async () => {
  await apiFetch("/api/auth/logout", { method: "POST" });
});

describe("usePublicStatusPage", () => {
  it("returns company_name/logo_url from the API response, not from mockData.companySettings", async () => {
    server.use(
      http.get("/api/status-pages/:id/public-preview", () =>
        HttpResponse.json({
          company: { name: "Acme Status", logo_url: "/uploads/logo" },
          services: [],
          incidents: { active: [], resolved: { items: [], total: 0, page: 1, page_size: 10 } },
        }),
      ),
    );
    await loginAsOwner();

    const { result } = renderHook(() => usePublicStatusPage("sp-1"), { wrapper: TestQueryProvider });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(result.current.data!.company_name).toBe("Acme Status");
    expect(result.current.data!.logo_url).toBe("/uploads/logo");
  });

  it("logo_url null in the API response is preserved, never replaced by a placeholder", async () => {
    server.use(
      http.get("/api/status-pages/:id/public-preview", () =>
        HttpResponse.json({
          company: { name: "Sem Logo Ltda.", logo_url: null },
          services: [],
          incidents: { active: [], resolved: { items: [], total: 0, page: 1, page_size: 10 } },
        }),
      ),
    );
    await loginAsOwner();

    const { result } = renderHook(() => usePublicStatusPage("sp-1"), { wrapper: TestQueryProvider });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(result.current.data!.company_name).toBe("Sem Logo Ltda.");
    expect(result.current.data!.logo_url).toBeNull();
  });

  // list-pagination T13: resolved incidents load progressively - page 1 on
  // mount, further pages only via loadMoreResolvedIncidents().
  function mockManyResolvedIncidents(total: number) {
    const all = Array.from({ length: total }, (_, i) => ({
      id: `res-${i + 1}`,
      title: `Resolvido ${i + 1}`,
      status: "resolved" as const,
      created_at: new Date().toISOString(),
      resolved_at: new Date().toISOString(),
      updates: [],
    }));
    server.use(
      http.get("/api/status-pages/:id/public-preview", ({ request }) => {
        const page = Number(new URL(request.url).searchParams.get("page")) || 1;
        const pageSize = 10;
        const start = (page - 1) * pageSize;
        return HttpResponse.json({
          company: { name: "Acme Status", logo_url: null },
          services: [],
          incidents: {
            active: [],
            resolved: { items: all.slice(start, start + pageSize), total: all.length, page, page_size: pageSize },
          },
        });
      }),
    );
  }

  it("loads only page 1 (10 items) of resolved incidents on mount", async () => {
    mockManyResolvedIncidents(11);
    await loginAsOwner();

    const { result } = renderHook(() => usePublicStatusPage("sp-1"), { wrapper: TestQueryProvider });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(result.current.data!.incidents.resolved).toHaveLength(10);
    expect(result.current.resolvedTotal).toBe(11);
    expect(result.current.hasMoreResolved).toBe(true);
  });

  it("loadMoreResolvedIncidents adds page 2 without replacing/reordering page 1", async () => {
    mockManyResolvedIncidents(11);
    await loginAsOwner();

    const { result } = renderHook(() => usePublicStatusPage("sp-1"), { wrapper: TestQueryProvider });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    const firstPageIds = result.current.data!.incidents.resolved.map((i) => i.id);

    await act(() => result.current.loadMoreResolvedIncidents());

    await waitFor(() => expect(result.current.data!.incidents.resolved).toHaveLength(11));
    expect(result.current.data!.incidents.resolved.slice(0, 10).map((i) => i.id)).toEqual(firstPageIds);
    expect(result.current.hasMoreResolved).toBe(false);
  });

  // public-status-time-range-selector T7: usePublicStatusPage threads its
  // `range` argument into the fetch URL and the React Query cache key.
  it("usePublicStatusPage(id, '90d') fetches ...&range=90d", async () => {
    let receivedRange: string | null = null;
    server.use(
      http.get("/api/status-pages/:id/public-preview", ({ request }) => {
        receivedRange = new URL(request.url).searchParams.get("range");
        return HttpResponse.json({
          company: { name: "Acme Status", logo_url: null },
          services: [],
          incidents: { active: [], resolved: { items: [], total: 0, page: 1, page_size: 10 } },
        });
      }),
    );
    await loginAsOwner();

    const { result } = renderHook(() => usePublicStatusPage("sp-1", "90d"), { wrapper: TestQueryProvider });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(receivedRange).toBe("90d");
  });

  it("default (no range) preserves today's behavior: fetches with range=24h", async () => {
    let receivedRange: string | null = null;
    server.use(
      http.get("/api/status-pages/:id/public-preview", ({ request }) => {
        receivedRange = new URL(request.url).searchParams.get("range");
        return HttpResponse.json({
          company: { name: "Acme Status", logo_url: null },
          services: [],
          incidents: { active: [], resolved: { items: [], total: 0, page: 1, page_size: 10 } },
        });
      }),
    );
    await loginAsOwner();

    const { result } = renderHook(() => usePublicStatusPage("sp-1"), { wrapper: TestQueryProvider });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(receivedRange).toBe("24h");
  });

  it("changing range between renders produces a distinct queryKey/fetch, not served from the 24h cache", async () => {
    const requestedRanges: string[] = [];
    server.use(
      http.get("/api/status-pages/:id/public-preview", ({ request }) => {
        requestedRanges.push(new URL(request.url).searchParams.get("range") ?? "");
        return HttpResponse.json({
          company: { name: "Acme Status", logo_url: null },
          services: [],
          incidents: { active: [], resolved: { items: [], total: 0, page: 1, page_size: 10 } },
        });
      }),
    );
    await loginAsOwner();

    const { result, rerender } = renderHook(({ range }: { range: "24h" | "90d" }) => usePublicStatusPage("sp-1", range), {
      wrapper: TestQueryProvider,
      initialProps: { range: "24h" },
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    rerender({ range: "90d" });
    await waitFor(() => expect(requestedRanges).toContain("90d"));

    expect(requestedRanges).toEqual(["24h", "90d"]);
  });
});
