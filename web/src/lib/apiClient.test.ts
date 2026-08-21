import { describe, it, expect, vi, afterEach } from "vitest";
import { apiFetch, ApiError, setUnauthorizedHandler, triggerUnauthorized } from "./apiClient";

afterEach(async () => {
  setUnauthorizedHandler(null);
  try {
    await apiFetch("/api/auth/logout", { method: "POST" });
  } catch {
    /* ignore */
  }
});

describe("apiClient (mock)", () => {
  it("login com credenciais válidas retorna admin e token", async () => {
    const res = await apiFetch<{ admin: { email: string; role: string }; token: string }>(
      "/api/auth/login",
      { method: "POST", body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }) }
    );
    expect(res.admin.email).toBe("owner@vane.app");
    expect(res.admin.role).toBe("owner");
    expect(res.token).toBeDefined();
  });

  it("login com credenciais inválidas rejeita com ApiError 401", async () => {
    await expect(
      apiFetch("/api/auth/login", {
        method: "POST",
        body: JSON.stringify({ email: "owner@vane.app", password: "wrong" }),
      })
    ).rejects.toMatchObject({ status: 401, message: "E-mail ou senha inválidos." });
  });

  it("GET /api/auth/me falha (401) quando não há sessão", async () => {
    await expect(apiFetch("/api/auth/me")).rejects.toBeInstanceOf(ApiError);
  });

  it("GET /api/auth/me retorna admin após login; logout limpa sessão", async () => {
    await apiFetch("/api/auth/login", {
      method: "POST",
      body: JSON.stringify({ email: "operator@vane.app", password: "demo1234" }),
    });
    const me = await apiFetch<{ admin: { role: string } }>("/api/auth/me");
    expect(me.admin.role).toBe("operator");

    await apiFetch("/api/auth/logout", { method: "POST" });
    await expect(apiFetch("/api/auth/me")).rejects.toBeInstanceOf(ApiError);
  });

  it("setUnauthorizedHandler + triggerUnauthorized dispara o handler registrado", () => {
    const handler = vi.fn();
    setUnauthorizedHandler(handler);
    triggerUnauthorized();
    expect(handler).toHaveBeenCalledTimes(1);
  });

  it("não dispara handler automaticamente sem chamada explícita", async () => {
    const handler = vi.fn();
    setUnauthorizedHandler(handler);
    await expect(apiFetch("/api/auth/me")).rejects.toBeInstanceOf(ApiError);
    expect(handler).not.toHaveBeenCalled();
  });
});
