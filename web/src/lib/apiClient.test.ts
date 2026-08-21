import { describe, it, expect, vi } from "vitest";
import { http, HttpResponse } from "msw";
import { server } from "../test/msw/server";
import { apiFetch, ApiError, setUnauthorizedHandler, triggerUnauthorized } from "./apiClient";

describe("apiClient (real fetch via MSW)", () => {
  it("login com credenciais válidas retorna token", async () => {
    const res = await apiFetch<{ token: string }>("/api/auth/login", {
      method: "POST",
      body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
    });
    expect(res.token).toBeDefined();
  });

  it("login com credenciais inválidas rejeita com ApiError 401", async () => {
    await expect(
      apiFetch("/api/auth/login", {
        method: "POST",
        body: JSON.stringify({ email: "owner@vane.app", password: "wrong" }),
      })
    ).rejects.toMatchObject({ status: 401, message: "invalid email or password" });
  });

  it("GET /api/auth/me falha (401) quando não há sessão", async () => {
    await expect(apiFetch("/api/auth/me")).rejects.toBeInstanceOf(ApiError);
  });

  it("GET /api/auth/me retorna a sessão após login; logout limpa a sessão", async () => {
    await apiFetch("/api/auth/login", {
      method: "POST",
      body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
    });
    const me = await apiFetch<{ role: string }>("/api/auth/me");
    expect(me.role).toBe("owner");

    await apiFetch("/api/auth/logout", { method: "POST" });
    await expect(apiFetch("/api/auth/me")).rejects.toBeInstanceOf(ApiError);
  });

  it("triggerUnauthorized dispara o handler registrado manualmente", () => {
    const handler = vi.fn();
    setUnauthorizedHandler(handler);
    triggerUnauthorized();
    expect(handler).toHaveBeenCalledTimes(1);
    setUnauthorizedHandler(null);
  });

  it("dispara o handler automaticamente em qualquer 401 real (AF-03)", async () => {
    const handler = vi.fn();
    setUnauthorizedHandler(handler);
    await expect(apiFetch("/api/auth/me")).rejects.toBeInstanceOf(ApiError);
    expect(handler).toHaveBeenCalledTimes(1);
    setUnauthorizedHandler(null);
  });

  it("não dispara o handler em erros não-401 (422)", async () => {
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
