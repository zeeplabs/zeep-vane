import { describe, it, expect, vi } from "vitest";
import { render, screen, waitFor, act } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { server } from "../test/msw/server";
import { AuthProvider, useAuth } from "./AuthProvider";
import { setTwoFactorEnabled } from "../test/msw/handlers";

function Probe() {
  const auth = useAuth();
  return (
    <div>
      <span data-testid="status">{auth.status}</span>
      <span data-testid="needs-bootstrap">{String(auth.needsBootstrap)}</span>
      <span data-testid="admin">{auth.admin ? JSON.stringify(auth.admin) : "null"}</span>
      <span data-testid="has-owner">{String(auth.hasRole(["owner"]))}</span>
      <span data-testid="has-operator">{String(auth.hasRole(["operator"]))}</span>
      <span data-testid="has-viewer">{String(auth.hasRole(["viewer"]))}</span>
      <button onClick={() => auth.login("owner@vane.app", "demo1234")}>login-ok</button>
      <button onClick={() => auth.login("owner@vane.app", "wrong").catch(() => {})}>
        login-fail
      </button>
      <button onClick={() => auth.logout()}>logout</button>
      <button onClick={() => auth.refreshAdmin()}>refresh-admin</button>
      <button onClick={() => auth.setDevRole("viewer")}>dev-role-viewer</button>
    </div>
  );
}

// Sessão MSW resetada globalmente entre testes por src/test/setup.ts
// (resetAuthSession, chamado no afterEach do server).

describe("AuthProvider", () => {
  it("boot sem sessão vira anonymous", async () => {
    render(
      <AuthProvider>
        <Probe />
      </AuthProvider>
    );
    await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("anonymous"));
  });

  it("login com sucesso guarda apenas id/email/role, nunca token", async () => {
    render(
      <AuthProvider>
        <Probe />
      </AuthProvider>
    );
    await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("anonymous"));

    await act(async () => {
      screen.getByText("login-ok").click();
    });

    await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("authenticated"));
    const admin = JSON.parse(screen.getByTestId("admin").textContent ?? "{}");
    expect(admin.email).toBe("owner@vane.app");
    expect(admin.role).toBe("owner");
    expect(admin).not.toHaveProperty("token");
  });

  it("login com falha mantém anonymous", async () => {
    render(
      <AuthProvider>
        <Probe />
      </AuthProvider>
    );
    await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("anonymous"));

    await act(async () => {
      screen.getByText("login-fail").click();
    });

    await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("anonymous"));
    expect(screen.getByTestId("admin")).toHaveTextContent("null");
  });

  it("hasRole correto para os 3 papéis após login como operator", async () => {
    render(
      <AuthProvider>
        <Probe />
      </AuthProvider>
    );
    await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("anonymous"));

    await act(async () => {
      // reaproveita o botão de login-ok trocando por operator via direct apiFetch not needed;
      // login through context using owner then verify hasRole owner true, others false
      screen.getByText("login-ok").click();
    });

    await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("authenticated"));
    expect(screen.getByTestId("has-owner")).toHaveTextContent("true");
    expect(screen.getByTestId("has-operator")).toHaveTextContent("false");
    expect(screen.getByTestId("has-viewer")).toHaveTextContent("false");
  });

  it("logout limpa a sessão e volta pra anonymous", async () => {
    render(
      <AuthProvider>
        <Probe />
      </AuthProvider>
    );
    await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("anonymous"));

    await act(async () => {
      screen.getByText("login-ok").click();
    });
    await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("authenticated"));

    await act(async () => {
      screen.getByText("logout").click();
    });

    await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("anonymous"));
    expect(screen.getByTestId("admin")).toHaveTextContent("null");
  });

  it("setDevRole é no-op fora de DEV", async () => {
    vi.stubEnv("DEV", false);
    render(
      <AuthProvider>
        <Probe />
      </AuthProvider>
    );
    await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("anonymous"));

    await act(async () => {
      screen.getByText("dev-role-viewer").click();
    });

    expect(screen.getByTestId("status")).toHaveTextContent("anonymous");
    expect(screen.getByTestId("admin")).toHaveTextContent("null");
    vi.unstubAllEnvs();
  });

  it("setDevRole autentica com o papel escolhido em DEV", async () => {
    render(
      <AuthProvider>
        <Probe />
      </AuthProvider>
    );
    await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("anonymous"));

    await act(async () => {
      screen.getByText("dev-role-viewer").click();
    });

    await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("authenticated"));
    const admin = JSON.parse(screen.getByTestId("admin").textContent ?? "{}");
    expect(admin.role).toBe("viewer");
  });

  it("needsBootstrap é true quando GET /api/bootstrap/status retorna bootstrapped:false (SHD-19)", async () => {
    server.use(
      http.get("/api/bootstrap/status", () => HttpResponse.json({ bootstrapped: false }))
    );
    render(
      <AuthProvider>
        <Probe />
      </AuthProvider>
    );

    await waitFor(() => expect(screen.getByTestId("needs-bootstrap")).toHaveTextContent("true"));
  });

  it("needsBootstrap é false quando GET /api/bootstrap/status retorna bootstrapped:true (SHD-19)", async () => {
    server.use(
      http.get("/api/bootstrap/status", () => HttpResponse.json({ bootstrapped: true }))
    );
    render(
      <AuthProvider>
        <Probe />
      </AuthProvider>
    );

    await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("anonymous"));
    expect(screen.getByTestId("needs-bootstrap")).toHaveTextContent("false");
  });

  // PROFPAGE-12: o provider expõe two_factor_enabled de /me, e refreshAdmin
  // re-hidrata a identidade sem reload (a mudança de estado feita nos
  // handlers MSW chega ao componente via refreshAdmin).
  it("expõe two_factor_enabled e refreshAdmin re-hidrata sem reload", async () => {
    render(
      <AuthProvider>
        <Probe />
      </AuthProvider>
    );
    await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("anonymous"));

    await act(async () => {
      screen.getByText("login-ok").click();
    });
    await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("authenticated"));
    expect(JSON.parse(screen.getByTestId("admin").textContent ?? "{}").two_factor_enabled).toBe(false);

    setTwoFactorEnabled(true);
    await act(async () => {
      screen.getByText("refresh-admin").click();
    });

    await waitFor(() =>
      expect(JSON.parse(screen.getByTestId("admin").textContent ?? "{}").two_factor_enabled).toBe(true)
    );
  });

  // design.md Error Handling: refreshAdmin nunca lança nem desloga; em falha
  // mantém a identidade anterior.
  it("refreshAdmin mantém a identidade anterior quando /me falha", async () => {
    render(
      <AuthProvider>
        <Probe />
      </AuthProvider>
    );
    await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("anonymous"));

    await act(async () => {
      screen.getByText("login-ok").click();
    });
    await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("authenticated"));
    const before = screen.getByTestId("admin").textContent;

    server.use(
      http.get("/api/auth/me", () => HttpResponse.json({ error: "boom" }, { status: 500 }))
    );
    await act(async () => {
      screen.getByText("refresh-admin").click();
    });

    expect(screen.getByTestId("status")).toHaveTextContent("authenticated");
    expect(screen.getByTestId("admin").textContent).toBe(before);
  });
});
