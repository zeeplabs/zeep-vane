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
  // PROFPAGE-04/05: o hook envia {name} e o refreshAdmin() re-hidrata a
  // identidade - o admin do contexto passa a mostrar o novo nome.
  it("useUpdateProfileName envia {name} e re-hidrata a identidade", async () => {
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

  // PROFPAGE-07: corpo exato {current_password,new_password} e sucesso 200.
  it("useChangePassword envia o corpo exato e resolve com status ok", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useChangePassword(), { wrapper: TestQueryProvider });

    const response = await result.current.mutateAsync({
      current_password: "demo1234",
      new_password: "nova-senha-forte",
    });

    expect(response).toEqual({ status: "ok" });
  });

  // PROFPAGE-09: 401 da senha atual errada chega como ApiError(status 401).
  it("useChangePassword propaga 401 como ApiError", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useChangePassword(), { wrapper: TestQueryProvider });

    await expect(
      result.current.mutateAsync({ current_password: "errada", new_password: "nova-senha-forte" })
    ).rejects.toMatchObject({ status: 401 });
  });

  // PROFPAGE-10: 422 da política de senha chega como ApiError(status 422).
  it("useChangePassword propaga 422 (política) como ApiError", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useChangePassword(), { wrapper: TestQueryProvider });

    await expect(
      result.current.mutateAsync({ current_password: "demo1234", new_password: "curta" })
    ).rejects.toMatchObject({ status: 422 });
  });
});
