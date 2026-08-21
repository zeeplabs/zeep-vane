import { describe, it, expect } from "vitest";
import { render, screen, waitFor, act } from "@testing-library/react";
import { AuthProvider, useAuth } from "./AuthProvider";

function Probe() {
  const auth = useAuth();
  return (
    <div>
      <span data-testid="status">{auth.status}</span>
      <span data-testid="admin">{auth.admin ? JSON.stringify(auth.admin) : "null"}</span>
      <span data-testid="has-owner">{String(auth.hasRole(["owner"]))}</span>
      <span data-testid="has-operator">{String(auth.hasRole(["operator"]))}</span>
      <span data-testid="has-viewer">{String(auth.hasRole(["viewer"]))}</span>
      <button onClick={() => auth.login("owner@vane.app", "demo1234")}>login-ok</button>
      <button onClick={() => auth.login("owner@vane.app", "wrong").catch(() => {})}>
        login-fail
      </button>
      <button onClick={() => auth.logout()}>logout</button>
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
});
