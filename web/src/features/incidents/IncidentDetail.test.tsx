import { describe, it, expect, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import "../../lib/i18n";
import { AuthProvider } from "../../auth/AuthProvider";
import { TestQueryProvider } from "../../test/queryClient";
import { server } from "../../test/msw/server";
import { apiFetch } from "../../lib/apiClient";
import { IncidentDetail } from "./IncidentDetail";

async function loginAs(email: string) {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email, password: "demo1234" }),
  });
}

afterEach(async () => {
  await apiFetch("/api/auth/logout", { method: "POST" });
});

function renderDetail(id: string) {
  return render(
    <MemoryRouter initialEntries={[`/incidents/${id}`]}>
      <TestQueryProvider>
        <AuthProvider>
          <Routes>
            <Route path="/incidents/:id" element={<IncidentDetail />} />
          </Routes>
        </AuthProvider>
      </TestQueryProvider>
    </MemoryRouter>
  );
}

describe("IncidentDetail", () => {
  it("timeline renders updates most recent first", async () => {
    await loginAs("owner@vane.app");
    renderDetail("inc-1");
    await screen.findByText(/Identificamos aumento de latência/);

    const bodies = screen.getAllByText(
      /Identificamos aumento de latência|Causa raiz identificada/
    );
    expect(bodies[0]).toHaveTextContent("Causa raiz identificada");
    expect(bodies[1]).toHaveTextContent("Identificamos aumento de latência");
  });

  it("adding 2 updates appears in reverse chronological order", async () => {
    await loginAs("owner@vane.app");
    renderDetail("inc-1");
    await screen.findByText(/Identificamos aumento de latência/);

    await userEvent.type(screen.getByLabelText("Novo update"), "Primeiro update novo");
    await userEvent.click(screen.getByRole("button", { name: "Publicar" }));
    await screen.findByText("Primeiro update novo");

    await userEvent.clear(screen.getByLabelText("Novo update"));
    await userEvent.type(screen.getByLabelText("Novo update"), "Segundo update novo");
    await userEvent.click(screen.getByRole("button", { name: "Publicar" }));
    await screen.findByText("Segundo update novo");

    const bodies = screen.getAllByText(/update novo/);
    expect(bodies[0]).toHaveTextContent("Segundo update novo");
    expect(bodies[1]).toHaveTextContent("Primeiro update novo");
  });

  it("marking as resolved moves the incident to history while keeping the timeline accessible", async () => {
    await loginAs("owner@vane.app");
    renderDetail("inc-1");
    await screen.findByText(/Identificamos aumento de latência/);

    await userEvent.click(screen.getByRole("button", { name: "Marcar como resolvido" }));

    await waitFor(() => expect(screen.getByText("Resolvido")).toBeInTheDocument());
    expect(screen.getByText(/Identificamos aumento de latência/)).toBeInTheDocument();
  });

  it("viewer doesn't see the update form or transition buttons", async () => {
    await loginAs("viewer@vane.app");
    renderDetail("inc-1");
    await screen.findByText(/Identificamos aumento de latência/);
    expect(screen.queryByLabelText("Novo update")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Marcar como resolvido" })).not.toBeInTheDocument();
  });

  // AI-19/AI-20/AI-21/AI-22: inc-1's fixture (mockData.ts) already carries a
  // pending_close_comment, so the banner shows out of the box for an owner.
  it("shows the close proposal banner with confirm/discard buttons (owner)", async () => {
    await loginAs("owner@vane.app");
    renderDetail("inc-1");

    expect(await screen.findByText(/Latência normalizada após rollback/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Confirmar encerramento" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Descartar" })).toBeInTheDocument();
  });

  it("inc-2, with no pending proposal, doesn't show the close banner", async () => {
    await loginAs("owner@vane.app");
    renderDetail("inc-2");

    await screen.findByText("Indisponibilidade parcial da API");
    expect(screen.queryByRole("button", { name: "Confirmar encerramento" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Descartar" })).not.toBeInTheDocument();
  });

  it("viewer sees the proposal banner but not the confirm/discard buttons", async () => {
    await loginAs("viewer@vane.app");
    renderDetail("inc-1");

    expect(await screen.findByText(/Latência normalizada após rollback/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Confirmar encerramento" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Descartar" })).not.toBeInTheDocument();
  });

  it("confirming close resolves the incident and the banner disappears", async () => {
    await loginAs("owner@vane.app");
    renderDetail("inc-1");

    await screen.findByRole("button", { name: "Confirmar encerramento" });
    await userEvent.click(screen.getByRole("button", { name: "Confirmar encerramento" }));

    await waitFor(() =>
      expect(screen.queryByRole("button", { name: "Confirmar encerramento" })).not.toBeInTheDocument()
    );
    expect(screen.getByText("Resolvido")).toBeInTheDocument();
    // The close comment is published as the final update (AI-20).
    expect(screen.getByText(/Latência normalizada após rollback/)).toBeInTheDocument();
  });

  it("discarding the proposal removes the banner without changing the incident status", async () => {
    await loginAs("owner@vane.app");
    renderDetail("inc-1");

    await screen.findByRole("button", { name: "Descartar" });
    await userEvent.click(screen.getByRole("button", { name: "Descartar" }));

    await waitFor(() =>
      expect(screen.queryByRole("button", { name: "Descartar" })).not.toBeInTheDocument()
    );
    expect(screen.queryByText("Resolvido")).not.toBeInTheDocument();
  });

  it("error confirming close shows an inline alert and keeps the banner", async () => {
    server.use(
      http.post("/api/incidents/:id/confirm-close", () =>
        HttpResponse.json({ error: "incident has no pending close proposal" }, { status: 422 })
      )
    );
    await loginAs("owner@vane.app");
    renderDetail("inc-1");

    await screen.findByRole("button", { name: "Confirmar encerramento" });
    await userEvent.click(screen.getByRole("button", { name: "Confirmar encerramento" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(/no pending close proposal/);
    expect(screen.getByRole("button", { name: "Confirmar encerramento" })).toBeInTheDocument();
  });

  it("error discarding the proposal shows an inline alert and keeps the banner", async () => {
    server.use(
      http.post("/api/incidents/:id/discard-close-proposal", () =>
        HttpResponse.json({ error: "incident has no pending close proposal" }, { status: 422 })
      )
    );
    await loginAs("owner@vane.app");
    renderDetail("inc-1");

    await screen.findByRole("button", { name: "Descartar" });
    await userEvent.click(screen.getByRole("button", { name: "Descartar" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(/no pending close proposal/);
    expect(screen.getByRole("button", { name: "Descartar" })).toBeInTheDocument();
  });
});
