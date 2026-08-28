import { describe, it, expect, vi } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch } from "../../lib/apiClient";
import {
  useAddIncidentUpdate,
  useIncidents,
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
      () => ({ updates: useIncidentUpdates("inc-1", 1), add: useAddIncidentUpdate("inc-1") }),
      { wrapper: TestQueryProvider }
    );
    await waitFor(() => expect(result.current.updates.isSuccess).toBe(true));
    const before = result.current.updates.data!.items.length;

    const response = await result.current.add.mutateAsync("Novo update de teste.");
    expect(Array.isArray(response)).toBe(true);

    await waitFor(() => expect(result.current.updates.data!.items.length).toBe(before + 1));
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

  it("useIncidents(1) usa queryKey com a página e busca /api/incidents?page=1, retornando o envelope Page completo", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    await loginAsOwner();
    const { result } = renderHook(() => useIncidents(1), { wrapper: TestQueryProvider });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    const call = fetchSpy.mock.calls.find(([url]) => String(url).includes("/api/incidents?page=1"));
    expect(call).toBeDefined();

    expect(result.current.data?.page).toBe(1);
    expect(result.current.data?.page_size).toBe(25);
    expect(Array.isArray(result.current.data?.items)).toBe(true);
    expect(typeof result.current.data?.total).toBe("number");

    fetchSpy.mockRestore();
  });

  it("useIncidentUpdates(id, 1) busca /api/incidents/:id/updates?page=1, retornando o envelope Page completo", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    await loginAsOwner();
    const { result } = renderHook(() => useIncidentUpdates("inc-1", 1), { wrapper: TestQueryProvider });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    const call = fetchSpy.mock.calls.find(([url]) =>
      String(url).includes("/api/incidents/inc-1/updates?page=1"),
    );
    expect(call).toBeDefined();

    expect(result.current.data?.page).toBe(1);
    expect(result.current.data?.page_size).toBe(25);
    expect(Array.isArray(result.current.data?.items)).toBe(true);

    fetchSpy.mockRestore();
  });
});
