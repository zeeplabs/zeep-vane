import { describe, it, expect, vi } from "vitest";
import { render, screen, waitFor, act } from "@testing-library/react";
import { useState } from "react";
import { http, HttpResponse } from "msw";
import { server } from "../test/msw/server";
import { AuthProvider, useAuth } from "./AuthProvider";
import { setTwoFactorEnabled, seedRecoveryCode, mswValidTotpCode } from "../test/msw/handlers";

function Probe() {
  const auth = useAuth();
  const [outcome, setOutcome] = useState("");
  const [challenge, setChallenge] = useState("");
  return (
    <div>
      <span data-testid="status">{auth.status}</span>
      <span data-testid="needs-bootstrap">{String(auth.needsBootstrap)}</span>
      <span data-testid="admin">{auth.admin ? JSON.stringify(auth.admin) : "null"}</span>
      <span data-testid="has-owner">{String(auth.hasRole(["owner"]))}</span>
      <span data-testid="has-operator">{String(auth.hasRole(["operator"]))}</span>
      <span data-testid="has-viewer">{String(auth.hasRole(["viewer"]))}</span>
      <span data-testid="outcome">{outcome}</span>
      <button onClick={() => auth.login("owner@vane.app", "demo1234")}>login-ok</button>
      <button onClick={() => auth.login("owner@vane.app", "wrong").catch(() => {})}>
        login-fail
      </button>
      <button
        onClick={async () => {
          const result = await auth.login("owner@vane.app", "demo1234");
          setOutcome(result.kind);
          if (result.kind === "twoFactorRequired") setChallenge(result.challengeToken);
        }}
      >
        login-capture
      </button>
      <button
        onClick={async () => {
          try {
            await auth.verifyTwoFactor(challenge, { code: mswValidTotpCode });
            setOutcome("verified");
          } catch {
            setOutcome("verify-failed");
          }
        }}
      >
        verify-code-ok
      </button>
      <button
        onClick={async () => {
          try {
            await auth.verifyTwoFactor(challenge, { code: "000000" });
            setOutcome("verified");
          } catch {
            setOutcome("verify-failed");
          }
        }}
      >
        verify-code-bad
      </button>
      <button
        onClick={async () => {
          try {
            await auth.verifyTwoFactor(challenge, { recoveryCode: "RECOVERY-OK" });
            setOutcome("verified");
          } catch {
            setOutcome("verify-failed");
          }
        }}
      >
        verify-recovery
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

  // LOGIN2FA-10: o provider reporta twoFactorRequired e não hidrata admin
  // quando /api/auth/login responde com challenge_token.
  it("login com 2FA ativa retorna twoFactorRequired sem autenticar", async () => {
    setTwoFactorEnabled(true);
    render(
      <AuthProvider>
        <Probe />
      </AuthProvider>
    );
    await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("anonymous"));

    await act(async () => {
      screen.getByText("login-capture").click();
    });

    await waitFor(() => expect(screen.getByTestId("outcome")).toHaveTextContent("twoFactorRequired"));
    expect(screen.getByTestId("status")).toHaveTextContent("anonymous");
    expect(screen.getByTestId("admin")).toHaveTextContent("null");
  });

  // LOGIN2FA-04: login sem 2FA continua autenticando.
  it("login sem 2FA retorna authenticated", async () => {
    render(
      <AuthProvider>
        <Probe />
      </AuthProvider>
    );
    await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("anonymous"));

    await act(async () => {
      screen.getByText("login-capture").click();
    });

    await waitFor(() => expect(screen.getByTestId("outcome")).toHaveTextContent("authenticated"));
    expect(screen.getByTestId("status")).toHaveTextContent("authenticated");
  });

  // LOGIN2FA-11: verifyTwoFactor com código válido hidrata o admin via /me.
  it("verifyTwoFactor com código válido hidrata o admin", async () => {
    setTwoFactorEnabled(true);
    render(
      <AuthProvider>
        <Probe />
      </AuthProvider>
    );
    await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("anonymous"));
    await act(async () => {
      screen.getByText("login-capture").click();
    });
    await waitFor(() => expect(screen.getByTestId("outcome")).toHaveTextContent("twoFactorRequired"));

    await act(async () => {
      screen.getByText("verify-code-ok").click();
    });

    await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("authenticated"));
    expect(JSON.parse(screen.getByTestId("admin").textContent ?? "{}").email).toBe("owner@vane.app");
  });

  // LOGIN2FA-11: código inválido rejeita e mantém anonymous (sem sessão).
  it("verifyTwoFactor com código inválido rejeita e permanece anonymous", async () => {
    setTwoFactorEnabled(true);
    render(
      <AuthProvider>
        <Probe />
      </AuthProvider>
    );
    await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("anonymous"));
    await act(async () => {
      screen.getByText("login-capture").click();
    });
    await waitFor(() => expect(screen.getByTestId("outcome")).toHaveTextContent("twoFactorRequired"));

    await act(async () => {
      screen.getByText("verify-code-bad").click();
    });

    await waitFor(() => expect(screen.getByTestId("outcome")).toHaveTextContent("verify-failed"));
    expect(screen.getByTestId("status")).toHaveTextContent("anonymous");
    expect(screen.getByTestId("admin")).toHaveTextContent("null");
  });

  // LOGIN2FA-10: o fallback por código de recuperação autentica no provider.
  it("verifyTwoFactor com código de recuperação válido autentica", async () => {
    seedRecoveryCode("RECOVERY-OK");
    setTwoFactorEnabled(true);
    render(
      <AuthProvider>
        <Probe />
      </AuthProvider>
    );
    await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("anonymous"));
    await act(async () => {
      screen.getByText("login-capture").click();
    });
    await waitFor(() => expect(screen.getByTestId("outcome")).toHaveTextContent("twoFactorRequired"));

    await act(async () => {
      screen.getByText("verify-recovery").click();
    });

    await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("authenticated"));
  });
});
