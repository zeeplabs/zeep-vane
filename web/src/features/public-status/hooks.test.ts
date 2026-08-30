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
  it("retorna company_name/logo_url da resposta da API, não de mockData.companySettings", async () => {
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

  it("logo_url null na resposta da API é preservado, nunca substituído por um placeholder", async () => {
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

  it("carrega apenas a página 1 (10 itens) dos incidentes resolvidos ao montar", async () => {
    mockManyResolvedIncidents(11);
    await loginAsOwner();

    const { result } = renderHook(() => usePublicStatusPage("sp-1"), { wrapper: TestQueryProvider });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(result.current.data!.incidents.resolved).toHaveLength(10);
    expect(result.current.resolvedTotal).toBe(11);
    expect(result.current.hasMoreResolved).toBe(true);
  });

  it("loadMoreResolvedIncidents adiciona a página 2 sem substituir/reordenar a página 1", async () => {
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
});
