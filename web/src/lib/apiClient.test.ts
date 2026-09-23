import { describe, it, expect, vi } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "../test/msw/server";
import { apiFetch, ApiError, setUnauthorizedHandler, triggerUnauthorized } from "./apiClient";

describe("apiClient (real fetch via MSW)", () => {
  it("login with valid credentials returns a token", async () => {
    const res = await apiFetch<{ token: string }>("/api/auth/login", {
      method: "POST",
      body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
    });
    expect(res.token).toBeDefined();
  });

  it("login with invalid credentials rejects with ApiError 401", async () => {
    await expect(
      apiFetch("/api/auth/login", {
        method: "POST",
        body: JSON.stringify({ email: "owner@vane.app", password: "wrong" }),
      })
    ).rejects.toMatchObject({ status: 401, message: "invalid email or password" });
  });

  it("GET /api/auth/me fails (401) when there is no session", async () => {
    await expect(apiFetch("/api/auth/me")).rejects.toBeInstanceOf(ApiError);
  });

  it("GET /api/auth/me returns the session after login; logout clears the session", async () => {
    await apiFetch("/api/auth/login", {
      method: "POST",
      body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
    });
    const me = await apiFetch<{ role: string }>("/api/auth/me");
    expect(me.role).toBe("owner");

    await apiFetch("/api/auth/logout", { method: "POST" });
    await expect(apiFetch("/api/auth/me")).rejects.toBeInstanceOf(ApiError);
  });

  it("triggerUnauthorized fires the manually registered handler", () => {
    const handler = vi.fn();
    setUnauthorizedHandler(handler);
    triggerUnauthorized();
    expect(handler).toHaveBeenCalledTimes(1);
    setUnauthorizedHandler(null);
  });

  it("fires the handler automatically on any real 401 (AF-03)", async () => {
    const handler = vi.fn();
    setUnauthorizedHandler(handler);
    await expect(apiFetch("/api/auth/me")).rejects.toBeInstanceOf(ApiError);
    expect(handler).toHaveBeenCalledTimes(1);
    setUnauthorizedHandler(null);
  });

  it("does not fire the handler on non-401 errors (422)", async () => {
    server.use(
      http.post("/api/domains", () => HttpResponse.json({ error: "Hostname é obrigatório." }, { status: 422 }))
    );
    const handler = vi.fn();
    setUnauthorizedHandler(handler);
    await expect(apiFetch("/api/domains", { method: "POST", body: "{}" })).rejects.toMatchObject({
      status: 422,
    });
    expect(handler).not.toHaveBeenCalled();
    setUnauthorizedHandler(null);
  });
});
