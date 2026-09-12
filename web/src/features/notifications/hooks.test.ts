import { describe, it, expect, afterEach } from "vitest";
import { createElement, type ReactNode } from "react";
import { renderHook, waitFor, act } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { TestQueryProvider } from "../../test/queryClient";
import { server } from "../../test/msw/server";
import { apiFetch } from "../../lib/apiClient";
import {
  useNotificationPreferences,
  useUpdateNotificationPreference,
} from "./hooks";

// useUpdateNotificationPreference reads/writes the react-query cache, so both
// hooks share one TestQueryProvider wrapper.
const wrapper = ({ children }: { children: ReactNode }) =>
  createElement(TestQueryProvider, null, children);

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

afterEach(async () => {
  await apiFetch("/api/auth/logout", { method: "POST" });
});

describe("notification preference hooks", () => {
  // NOTIFPREF-13: a brand-new user gets the documented defaults.
  it("useNotificationPreferences returns the documented defaults for a fresh user", async () => {
    await loginAsOwner();

    const { result } = renderHook(() => useNotificationPreferences(), { wrapper });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toEqual({
      incident_opened: true,
      incident_resolved: true,
      weekly_digest: false,
    });
  });

  // NOTIFPREF-14: a single-key PATCH persists that key only.
  it("useUpdateNotificationPreference PATCHes a single key and persists it", async () => {
    await loginAsOwner();

    const { result } = renderHook(
      () => ({
        prefs: useNotificationPreferences(),
        update: useUpdateNotificationPreference(),
      }),
      { wrapper },
    );
    await waitFor(() => expect(result.current.prefs.isSuccess).toBe(true));

    await act(async () => {
      await result.current.update.mutateAsync({ weekly_digest: true });
    });

    // The query cache reflects the persisted response: only weekly_digest
    // changed, the other two stay at their defaults.
    await waitFor(() =>
      expect(result.current.prefs.data).toEqual({
        incident_opened: true,
        incident_resolved: true,
        weekly_digest: true,
      }),
    );
  });

  // NOTIFPREF-14: a failed PATCH rolls the optimistic change back.
  it("rolls the optimistic toggle back when the PATCH fails", async () => {
    await loginAsOwner();
    server.use(
      http.patch("/api/auth/notification-preferences", () =>
        HttpResponse.json({ error: "boom" }, { status: 500 }),
      ),
    );

    const { result } = renderHook(
      () => ({
        prefs: useNotificationPreferences(),
        update: useUpdateNotificationPreference(),
      }),
      { wrapper },
    );
    await waitFor(() => expect(result.current.prefs.isSuccess).toBe(true));
    expect(result.current.prefs.data?.weekly_digest).toBe(false);

    await act(async () => {
      await result.current.update.mutateAsync({ weekly_digest: true }).catch(() => undefined);
    });

    await waitFor(() => expect(result.current.prefs.data?.weekly_digest).toBe(false));
  });

  // 401 without a session: the query hook surfaces the error.
  it("useNotificationPreferences surfaces a 401 when unauthenticated", async () => {
    const { result } = renderHook(() => useNotificationPreferences(), { wrapper });

    await waitFor(() => expect(result.current.isError).toBe(true));
    expect((result.current.error as { status?: number }).status).toBe(401);
  });

  // 401 without a session: the mutation rejects.
  it("useUpdateNotificationPreference rejects on 401 without a session", async () => {
    const { result } = renderHook(() => useUpdateNotificationPreference(), { wrapper });

    await act(async () => {
      await expect(
        result.current.mutateAsync({ weekly_digest: true }),
      ).rejects.toMatchObject({ status: 401 });
    });
  });
});
