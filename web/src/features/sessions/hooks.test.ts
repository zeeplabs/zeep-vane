import { describe, it, expect, afterEach } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch } from "../../lib/apiClient";
import { useRevokeSession, useSessions } from "./hooks";

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

afterEach(async () => {
  await apiFetch("/api/auth/logout", { method: "POST" });
});

describe("sessions hooks", () => {
  it("useSessions retorna a lista do usuário com a flag current marcada na sessão do cookie", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useSessions(), { wrapper: TestQueryProvider });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    const sessions = result.current.data!;
    // admin-1 (owner) tem 2 sessões no seed (sess-1 e sess-2); sess-3
    // pertence a admin-2 e nunca aparece aqui (filtro por user_id).
    expect(sessions).toHaveLength(2);
    // O mock de login define currentSessionId como o primeiro match do
    // user no array seed (sess-1). A listagem ordena por created_at DESC
    // (sess-1 é mais recente que sess-2 no seed) e marca current:true
    // exatamente em sess-1, espelhando o que o backend faz via comparação
    // com o claim sid do JWT.
    const current = sessions.find((s) => s.current);
    const other = sessions.find((s) => !s.current);
    expect(current).toBeDefined();
    expect(other).toBeDefined();
    expect(current!.id).toBe("sess-1");
    expect(other!.id).toBe("sess-2");
  });

  it("useRevokeSession chama DELETE e a próxima listagem reflete a revogação", async () => {
    await loginAsOwner();
    const { result } = renderHook(
      () => {
        const sessions = useSessions();
        const revoke = useRevokeSession();
        return { sessions, revoke };
      },
      { wrapper: TestQueryProvider }
    );
    await waitFor(() => expect(result.current.sessions.isSuccess).toBe(true));
    expect(result.current.sessions.data).toHaveLength(2);

    result.current.revoke.mutate("sess-2");

    // Após a mutação, o handler setou revoked_at em sess-2; o onSuccess
    // invalidou a query ["sessions"], então o useQuery refaz o GET e o
    // filtro do mock (revoked_at IS NULL) remove a linha da resposta.
    await waitFor(() => expect(result.current.sessions.data).toHaveLength(1));
    expect(result.current.sessions.data![0].id).toBe("sess-1");
  });
});
