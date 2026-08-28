import { describe, it, expect, afterEach } from "vitest";
import { http, HttpResponse } from "msw";
import { renderHook, waitFor } from "@testing-library/react";
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
          incidents: { active: [], resolved: [] },
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
          incidents: { active: [], resolved: [] },
        }),
      ),
    );
    await loginAsOwner();

    const { result } = renderHook(() => usePublicStatusPage("sp-1"), { wrapper: TestQueryProvider });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(result.current.data!.company_name).toBe("Sem Logo Ltda.");
    expect(result.current.data!.logo_url).toBeNull();
  });
});
