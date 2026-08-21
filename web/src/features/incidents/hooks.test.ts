import { describe, it, expect } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch } from "../../lib/apiClient";
import {
  useAddIncidentUpdate,
  useIncidentUpdates,
  useTransitionIncident,
} from "./hooks";

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

describe("incidents hooks", () => {
  it("useAddIncidentUpdate invalida a timeline do incidente em sucesso (resposta é a lista completa)", async () => {
    await loginAsOwner();
    const { result } = renderHook(
      () => ({ updates: useIncidentUpdates("inc-1"), add: useAddIncidentUpdate("inc-1") }),
      { wrapper: TestQueryProvider }
    );
    await waitFor(() => expect(result.current.updates.isSuccess).toBe(true));
    const before = result.current.updates.data!.length;

    const response = await result.current.add.mutateAsync("Novo update de teste.");
    expect(Array.isArray(response)).toBe(true);

    await waitFor(() => expect(result.current.updates.data!.length).toBe(before + 1));
  });

  it("useTransitionIncident aceita reabertura (resolved -> estado anterior)", async () => {
    await loginAsOwner();
    const { result } = renderHook(() => useTransitionIncident("inc-2"), {
      wrapper: TestQueryProvider,
    });

    const reopened = await result.current.mutateAsync("investigating");
    expect(reopened.status).toBe("investigating");
    expect(reopened.resolved_at).toBeNull();
  });
});
