import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { apiFetch } from "../../lib/apiClient";
import { PublicStatusPage } from "./PublicStatusPage";

// The preview endpoint (I12) sits behind requireAuth - unlike the real
// production public page (served by the Go backend directly via Host
// header, never through this SPA route). Any authenticated role can
// preview, so a fixed owner login is enough setup for every case here.
async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

async function renderAt(path: string) {
  await loginAsOwner();
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/status/:id" element={<PublicStatusPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("PublicStatusPage", () => {
  it("página sem incidentes mostra banda de operacional e nenhum incidente ativo", async () => {
    await renderAt("/status/sp-4");

    expect(await screen.findByText("Todos os sistemas operacionais")).toBeInTheDocument();
    expect(screen.getByText("Fila de processamento")).toBeInTheDocument();
    expect(screen.queryByText("Incidente em andamento")).not.toBeInTheDocument();
    expect(await screen.findByText("Nenhum incidente nos últimos 90 dias.")).toBeInTheDocument();
  });

  it("página com incidente ativo mostra card no topo e permite expandir a linha do tempo", async () => {
    await renderAt("/status/sp-1");

    expect(await screen.findByText("Incidente em andamento")).toBeInTheDocument();
    expect(screen.getByText("Latência elevada no Checkout")).toBeInTheDocument();

    const [toggleActiveTimeline] = screen.getAllByRole("button", { name: "Ver linha do tempo" });
    await userEvent.click(toggleActiveTimeline);
    expect(
      await screen.findByText("Causa raiz identificada: pico de tráfego não previsto. Monitorando estabilização."),
    ).toBeInTheDocument();
  });

  it("página com histórico resolvido mostra card de incidente resolvido", async () => {
    await renderAt("/status/sp-1");

    expect(await screen.findByText("Indisponibilidade parcial da API")).toBeInTheDocument();
    expect(screen.getByText(/Resolvido \d{2} \w{3}, \d{2}:\d{2}/)).toBeInTheDocument();
  });

  it("status page inexistente ou não publicada mostra página não encontrada", async () => {
    await renderAt("/status/sp-2");

    expect(await screen.findByText("Página não encontrada.")).toBeInTheDocument();
  });
});
