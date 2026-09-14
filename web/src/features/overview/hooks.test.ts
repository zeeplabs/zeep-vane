import { describe, it, expect } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { TestQueryProvider } from "../../test/queryClient";
import { server } from "../../test/msw/server";
import { apiFetch } from "../../lib/apiClient";
import { useOverview } from "./hooks";

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

describe("overview hooks", () => {
  it("useOverview expõe o OverviewResponse com os 6 campos", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useOverview(), { wrapper: TestQueryProvider });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    const data = result.current.data!;
    expect(data).toHaveProperty("uptime_avg_30d");
    expect(data).toHaveProperty("open_incidents");
    expect(data).toHaveProperty("unhealthy_services");
    expect(data).toHaveProperty("verified_domains");
    expect(data).toHaveProperty("uptime_series");
    expect(data).toHaveProperty("recent_incidents");
    expect(data.uptime_series).toHaveLength(14);
  });

  it("useOverview surfaceia isError quando o endpoint responde 500", async () => {
    await loginAsOwner();
    server.use(
      http.get("/api/overview", () =>
        HttpResponse.json({ error: "internal server error" }, { status: 500 }),
      ),
    );

    const { result } = renderHook(() => useOverview(), { wrapper: TestQueryProvider });
    await waitFor(() => expect(result.current.isError).toBe(true));
  });
});
