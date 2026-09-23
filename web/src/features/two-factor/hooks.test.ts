import { describe, it, expect, afterEach } from "vitest";
import { createElement, type ReactNode } from "react";
import { renderHook, waitFor, act } from "@testing-library/react";
import { TestQueryProvider } from "../../test/queryClient";
import { AuthProvider, useAuth } from "../../auth/AuthProvider";
import { apiFetch } from "../../lib/apiClient";
import { mswValidTotpCode, setTwoFactorEnabled } from "../../test/msw/handlers";
import { useConfirm2FA, useDisable2FA, useEnroll2FA } from "./hooks";

// useDisable2FA reads refreshAdmin from the auth context, so its test renders
// both providers; enroll/confirm only need react-query.
const withAuthWrapper = ({ children }: { children: ReactNode }) =>
  createElement(TestQueryProvider, null, createElement(AuthProvider, null, children));

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

afterEach(async () => {
  await apiFetch("/api/auth/logout", { method: "POST" });
});

describe("two-factor hooks", () => {
  // PROFPAGE-13: success returns the secret and the otpauth:// URI that the
  // drawer renders as a QR code.
  it("useEnroll2FA returns secret and otpauth_uri", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useEnroll2FA(), { wrapper: TestQueryProvider });

    const response = await result.current.mutateAsync();

    expect(response.secret).toBeTruthy();
    expect(response.otpauth_uri.startsWith("otpauth://")).toBe(true);
  });

  // PROFPAGE-13 edge case: 409 when a confirmed enrollment already exists.
  it("useEnroll2FA propagates 409 when already enabled", async () => {
    await loginAsOwner();
    setTwoFactorEnabled(true);
    const { result } = renderHook(() => useEnroll2FA(), { wrapper: TestQueryProvider });

    await expect(result.current.mutateAsync()).rejects.toMatchObject({ status: 409 });
  });

  // PROFPAGE-14: confirm returns exactly 10 recovery codes.
  it("useConfirm2FA returns 10 recovery codes", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useConfirm2FA(), { wrapper: TestQueryProvider });

    const response = await result.current.mutateAsync(mswValidTotpCode);

    expect(response.recovery_codes).toHaveLength(10);
    for (const code of response.recovery_codes) {
      expect(typeof code).toBe("string");
    }
  });

  // PROFPAGE-15: invalid code arrives as ApiError(status 422).
  it("useConfirm2FA propagates 422 on invalid code", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useConfirm2FA(), { wrapper: TestQueryProvider });

    await expect(result.current.mutateAsync("000000")).rejects.toMatchObject({ status: 422 });
  });

  // PROFPAGE-17: disabling with the correct password returns ok and
  // refreshAdmin re-hydrates the identity with two_factor_enabled=false.
  it("useDisable2FA disables and re-hydrates the identity", async () => {
    await loginAsOwner();
    setTwoFactorEnabled(true);
    const { result } = renderHook(
      () => {
        const auth = useAuth();
        const disable = useDisable2FA();
        return { auth, disable };
      },
      { wrapper: withAuthWrapper }
    );
    await waitFor(() => expect(result.current.auth.admin?.two_factor_enabled).toBe(true));

    let response: { status: string } | undefined;
    await act(async () => {
      response = await result.current.disable.mutateAsync("demo1234");
    });

    expect(response).toEqual({ status: "ok" });
    await waitFor(() => expect(result.current.auth.admin?.two_factor_enabled).toBe(false));
  });

  // PROFPAGE-18: wrong password arrives as ApiError(status 401) and 2FA
  // stays enabled.
  it("useDisable2FA propagates 401 on wrong password", async () => {
    await loginAsOwner();
    setTwoFactorEnabled(true);
    const { result } = renderHook(
      () => {
        const auth = useAuth();
        const disable = useDisable2FA();
        return { auth, disable };
      },
      { wrapper: withAuthWrapper }
    );
    await waitFor(() => expect(result.current.auth.admin?.two_factor_enabled).toBe(true));

    await act(async () => {
      await expect(result.current.disable.mutateAsync("errada")).rejects.toMatchObject({ status: 401 });
    });

    expect(result.current.auth.admin?.two_factor_enabled).toBe(true);
  });
});
