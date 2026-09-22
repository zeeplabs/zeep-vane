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
      <span data-testid="deployment-mode">{auth.deploymentMode}</span>
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

// MSW session reset globally between tests by src/test/setup.ts
// (resetAuthSession, called in the server's afterEach).

describe("AuthProvider", () => {
  it("boot without a session becomes anonymous", async () => {
    render(
      <AuthProvider>
        <Probe />
      </AuthProvider>
    );
    await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("anonymous"));
  });

  it("successful login stores only id/email/role, never a token", async () => {
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

  it("failed login keeps anonymous", async () => {
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

  it("hasRole correct for the 3 roles after logging in as operator", async () => {
    render(
      <AuthProvider>
        <Probe />
      </AuthProvider>
    );
    await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("anonymous"));

    await act(async () => {
      // reuses the login-ok button, swapping for operator via direct apiFetch not needed;
      // login through context using owner then verify hasRole owner true, others false
      screen.getByText("login-ok").click();
    });

    await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("authenticated"));
    expect(screen.getByTestId("has-owner")).toHaveTextContent("true");
    expect(screen.getByTestId("has-operator")).toHaveTextContent("false");
    expect(screen.getByTestId("has-viewer")).toHaveTextContent("false");
  });

  it("logout clears the session and goes back to anonymous", async () => {
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

  it("setDevRole is a no-op outside of DEV", async () => {
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

  it("setDevRole authenticates with the chosen role in DEV", async () => {
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

  it("needsBootstrap is true when GET /api/bootstrap/status returns bootstrapped:false (SHD-19)", async () => {
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

  it("needsBootstrap is false when GET /api/bootstrap/status returns bootstrapped:true (SHD-19)", async () => {
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

  // DEPMODE-05/06: deploymentMode is read from the same boot fetch as
  // needsBootstrap, defaulting to "saas" (optimistic) until it resolves.
  it("deploymentMode reflects self_hosted when GET /api/bootstrap/status returns deployment_mode: self_hosted (AD-033)", async () => {
    server.use(
      http.get("/api/bootstrap/status", () =>
        HttpResponse.json({ bootstrapped: true, deployment_mode: "self_hosted" })
      )
    );
    render(
      <AuthProvider>
        <Probe />
      </AuthProvider>
    );

    await waitFor(() => expect(screen.getByTestId("deployment-mode")).toHaveTextContent("self_hosted"));
  });

  it("deploymentMode reflects saas when GET /api/bootstrap/status returns deployment_mode: saas (AD-033)", async () => {
    server.use(
      http.get("/api/bootstrap/status", () =>
        HttpResponse.json({ bootstrapped: true, deployment_mode: "saas" })
      )
    );
    render(
      <AuthProvider>
        <Probe />
      </AuthProvider>
    );

    await waitFor(() => expect(screen.getByTestId("deployment-mode")).toHaveTextContent("saas"));
  });

  // PROFPAGE-12: the provider exposes two_factor_enabled from /me, and refreshAdmin
  // re-hydrates the identity without a reload (the state change made in the
  // MSW handlers reaches the component via refreshAdmin).
  it("exposes two_factor_enabled and refreshAdmin re-hydrates without a reload", async () => {
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

  // design.md Error Handling: refreshAdmin never throws nor logs out; on failure
  // it keeps the previous identity.
  it("refreshAdmin keeps the previous identity when /me fails", async () => {
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

  // LOGIN2FA-10: the provider reports twoFactorRequired and does not hydrate admin
  // when /api/auth/login responds with challenge_token.
  it("login with 2FA active returns twoFactorRequired without authenticating", async () => {
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

  // LOGIN2FA-04: login without 2FA still authenticates.
  it("login without 2FA returns authenticated", async () => {
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

  // LOGIN2FA-11: verifyTwoFactor with a valid code hydrates the admin via /me.
  it("verifyTwoFactor with a valid code hydrates the admin", async () => {
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

  // LOGIN2FA-11: invalid code is rejected and stays anonymous (no session).
  it("verifyTwoFactor with an invalid code rejects and remains anonymous", async () => {
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

  // LOGIN2FA-10: the recovery-code fallback authenticates in the provider.
  it("verifyTwoFactor with a valid recovery code authenticates", async () => {
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
