import { describe, it, expect } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { TestQueryProvider } from "../../test/queryClient";
import { server } from "../../test/msw/server";
import { seedAuditLogEntries } from "../../test/msw/handlers";
import { apiFetch } from "../../lib/apiClient";
import { useOverview, useRecentActivity } from "./hooks";

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

describe("overview hooks", () => {
  it("useOverview exposes the OverviewResponse with its 6 fields", async () => {
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

  it("useOverview surfaces isError when the endpoint responds with 500", async () => {
    await loginAsOwner();
    server.use(
      http.get("/api/overview", () =>
        HttpResponse.json({ error: "internal server error" }, { status: 500 }),
      ),
    );

    const { result } = renderHook(() => useOverview(), { wrapper: TestQueryProvider });
    await waitFor(() => expect(result.current.isError).toBe(true));
  });

  // recent-team-activity T15/ACTIVITY-09: useRecentActivity fetches
  // GET /api/audit-log?limit=5 as its own query, independent of useOverview.
  it("useRecentActivity returns the audit-log entries from the fixture", async () => {
    await loginAsOwner();
    seedAuditLogEntries([
      {
        action: "invited",
        target_label: "novo.membro@acme.health",
        actor_name: "Ana Silva",
        actor_deleted: false,
        created_at: "2026-09-10T12:00:00Z",
      },
    ]);

    const { result } = renderHook(() => useRecentActivity(), { wrapper: TestQueryProvider });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(result.current.data).toEqual([
      {
        action: "invited",
        target_label: "novo.membro@acme.health",
        actor_name: "Ana Silva",
        actor_deleted: false,
        created_at: "2026-09-10T12:00:00Z",
      },
    ]);
  });

  it("useRecentActivity surfaces isError when the endpoint responds with 500", async () => {
    await loginAsOwner();
    server.use(
      http.get("/api/audit-log", () =>
        HttpResponse.json({ error: "internal server error" }, { status: 500 }),
      ),
    );

    const { result } = renderHook(() => useRecentActivity(), { wrapper: TestQueryProvider });
    await waitFor(() => expect(result.current.isError).toBe(true));
  });
});
