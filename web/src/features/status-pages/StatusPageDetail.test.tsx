import { describe, it, expect, afterEach, vi } from "vitest";
import { render, screen, act } from "@testing-library/react";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import "../../lib/i18n";
import { AuthProvider } from "../../auth/AuthProvider";
import { TestQueryProvider } from "../../test/queryClient";
import * as apiClient from "../../lib/apiClient";
import { StatusPageDetail } from "./StatusPageDetail";

async function loginAsOwner() {
  await apiClient.apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

afterEach(async () => {
  vi.useRealTimers();
  await apiClient.apiFetch("/api/auth/logout", { method: "POST" });
});

function renderDetail(id: string) {
  return render(
    <MemoryRouter initialEntries={[`/status-pages/${id}`]}>
      <TestQueryProvider>
        <AuthProvider>
          <Routes>
            <Route path="/status-pages/:id" element={<StatusPageDetail />} />
          </Routes>
        </AuthProvider>
      </TestQueryProvider>
    </MemoryRouter>
  );
}

describe("StatusPageDetail", () => {
  it("estado published exibe a URL pública e não faz polling adicional", async () => {
    await loginAsOwner();
    const spy = vi.spyOn(apiClient, "apiFetch");
    renderDetail("sp-1");

    expect(await screen.findByText("Publicada")).toBeInTheDocument();
    expect(await screen.findByText(/https:\/\/status\.status\.acme\.com/)).toBeInTheDocument();

    const callsAfterLoad = spy.mock.calls.filter((c) => c[0] === "/api/status-pages").length;

    vi.useFakeTimers();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000);
    });

    const callsAfterWait = spy.mock.calls.filter((c) => c[0] === "/api/status-pages").length;
    expect(callsAfterWait).toBe(callsAfterLoad);
  });

  it("estado tls_failed exibe o motivo da falha e não faz polling adicional", async () => {
    await loginAsOwner();
    const spy = vi.spyOn(apiClient, "apiFetch");
    renderDetail("sp-3");

    expect(await screen.findByText("Falha")).toBeInTheDocument();
    expect(
      screen.getByText("Falha ao validar propriedade do domínio via DNS-01.")
    ).toBeInTheDocument();

    const callsAfterLoad = spy.mock.calls.filter((c) => c[0] === "/api/status-pages").length;

    vi.useFakeTimers();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000);
    });

    const callsAfterWait = spy.mock.calls.filter((c) => c[0] === "/api/status-pages").length;
    expect(callsAfterWait).toBe(callsAfterLoad);
  });
});
