import { describe, it, expect, afterEach } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch } from "../../lib/apiClient";
import { useRevokeSession, useSessions } from "./hooks";

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

afterEach(async () => {
  await apiFetch("/api/auth/logout", { method: "POST" });
});

describe("sessions hooks", () => {
  it("useSessions returns the user's list with the current flag set on the cookie's session", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useSessions(), { wrapper: TestQueryProvider });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    const sessions = result.current.data!;
    // admin-1 (owner) has 2 sessions in the seed (sess-1 and sess-2); sess-3
    // belongs to admin-2 and never shows up here (filtered by user_id).
    expect(sessions).toHaveLength(2);
    // The login mock sets currentSessionId to the user's first match in
    // the seed array (sess-1). The listing sorts by created_at DESC
    // (sess-1 is more recent than sess-2 in the seed) and marks
    // current:true on exactly sess-1, mirroring what the backend does by
    // comparing against the JWT's sid claim.
    const current = sessions.find((s) => s.current);
    const other = sessions.find((s) => !s.current);
    expect(current).toBeDefined();
    expect(other).toBeDefined();
    expect(current!.id).toBe("sess-1");
    expect(other!.id).toBe("sess-2");
  });

  it("useRevokeSession calls DELETE and the next listing reflects the revocation", async () => {
    await loginAsOwner();
    const { result } = renderHook(
      () => {
        const sessions = useSessions();
        const revoke = useRevokeSession();
        return { sessions, revoke };
      },
      { wrapper: TestQueryProvider }
    );
    await waitFor(() => expect(result.current.sessions.isSuccess).toBe(true));
    expect(result.current.sessions.data).toHaveLength(2);

    result.current.revoke.mutate("sess-2");

    // After the mutation, the handler set revoked_at on sess-2; onSuccess
    // invalidated the ["sessions"] query, so useQuery redoes the GET and
    // the mock's filter (revoked_at IS NULL) removes the row from the
    // response.
    await waitFor(() => expect(result.current.sessions.data).toHaveLength(1));
    expect(result.current.sessions.data![0].id).toBe("sess-1");
  });

  it("the mock response mirrors the backend's exact shape (no mock-only fields)", async () => {
    await loginAsOwner();
    const raw = await apiFetch<Record<string, unknown>[]>("/api/auth/sessions");
    expect(raw.length).toBeGreaterThan(0);

    // Backend `SessionView` (internal/api/sessions_handler.go:45-52) only
    // exposes id/user_agent?/ip?/created_at/last_seen_at?/current. Fields
    // that only exist in the mock seed (user_id/revoked_at) must not leak
    // into the response - this is exactly the drift AGENTS.md §5 forbids.
    for (const forbidden of ["user_id", "revoked_at", "expires_at"]) {
      expect(Object.keys(raw[0])).not.toContain(forbidden);
    }
    const keys = Object.keys(raw[0]);
    expect(keys).toContain("id");
    expect(keys).toContain("created_at");
    expect(keys).toContain("current");
  });
});
