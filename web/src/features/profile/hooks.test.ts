import { describe, it, expect, afterEach } from "vitest";
import { createElement, type ReactNode } from "react";
import { renderHook, waitFor, act } from "@testing-library/react";
import { TestQueryProvider } from "../../test/queryClient";
import { AuthProvider, useAuth } from "../../auth/AuthProvider";
import { apiFetch } from "../../lib/apiClient";
import { useChangePassword, useUpdateProfileName } from "./hooks";

// useUpdateProfileName reads refreshAdmin from the auth context, so its test
// renders both providers; the password hook only needs react-query.
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

describe("profile hooks", () => {
  // PROFPAGE-04/05: the hook sends {name} and refreshAdmin() re-hydrates the
  // identity - the context's admin then shows the new name.
  it("useUpdateProfileName sends {name} and re-hydrates the identity", async () => {
    await loginAsOwner();
    const { result } = renderHook(
      () => {
        const auth = useAuth();
        const update = useUpdateProfileName();
        return { auth, update };
      },
      { wrapper: withAuthWrapper }
    );
    await waitFor(() => expect(result.current.auth.admin).not.toBeNull());
    expect(result.current.auth.admin?.name).toBe("Ana Owner");

    await act(async () => {
      await result.current.update.mutateAsync("Ana Silva");
    });

    await waitFor(() => expect(result.current.auth.admin?.name).toBe("Ana Silva"));
  });

  // PROFPAGE-07: exact body {current_password,new_password} and a 200 success.
  it("useChangePassword sends the exact body and resolves with status ok", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useChangePassword(), { wrapper: TestQueryProvider });

    const response = await result.current.mutateAsync({
      current_password: "demo1234",
      new_password: "nova-senha-forte",
    });

    expect(response).toEqual({ status: "ok" });
  });

  // PROFPAGE-09: a 401 from a wrong current password arrives as ApiError(status 401).
  it("useChangePassword propagates 401 as ApiError", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useChangePassword(), { wrapper: TestQueryProvider });

    await expect(
      result.current.mutateAsync({ current_password: "errada", new_password: "nova-senha-forte" })
    ).rejects.toMatchObject({ status: 401 });
  });

  // PROFPAGE-10: a 422 from the password policy arrives as ApiError(status 422).
  it("useChangePassword propagates 422 (policy) as ApiError", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useChangePassword(), { wrapper: TestQueryProvider });

    await expect(
      result.current.mutateAsync({ current_password: "demo1234", new_password: "curta" })
    ).rejects.toMatchObject({ status: 422 });
  });
});
