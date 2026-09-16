import { describe, it, expect, afterEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { http, HttpResponse, delay } from "msw";
import "../../lib/i18n";
import { AuthProvider } from "../../auth/AuthProvider";
import { TestQueryProvider } from "../../test/queryClient";
import { server } from "../../test/msw/server";
import { apiFetch } from "../../lib/apiClient";
import { ServicesSection } from "./ServicesSection";

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

afterEach(async () => {
  await apiFetch("/api/auth/logout", { method: "POST" });
});

function renderSection() {
  return render(
    <MemoryRouter>
      <TestQueryProvider>
        <AuthProvider>
          <ServicesSection />
        </AuthProvider>
      </TestQueryProvider>
    </MemoryRouter>,
  );
}

describe("ServicesSection", () => {
  // SKEL-04/05: while /api/services is loading, skeleton rows render (not
  // the old "Carregando…" paragraph as visible content) inside an
  // aria-busy container that still carries the sr-only loading string.
  it("mostra skeletons (não o texto) enquanto /api/services carrega", async () => {
    server.use(
      http.get("/api/services", async () => {
        await delay("infinite");
        return HttpResponse.json({ items: [], total: 0, page: 1, page_size: 20 });
      }),
    );
    await loginAsOwner();
    renderSection();

    const srText = await screen.findByText("Carregando…");
    expect(srText.className).toContain("sr-only");
    expect(srText.closest('[aria-busy="true"]')).toBeInTheDocument();
    expect(screen.getAllByTestId("skeleton").length).toBeGreaterThan(0);
  });

  // SKEL-06: once the fetch resolves, skeletons are gone and the real
  // content (or its empty state) takes over.
  it("remove os skeletons assim que /api/services termina de carregar", async () => {
    await loginAsOwner();
    renderSection();

    await screen.findByText("API pública");
    expect(screen.queryAllByTestId("skeleton")).toHaveLength(0);
  });
});
