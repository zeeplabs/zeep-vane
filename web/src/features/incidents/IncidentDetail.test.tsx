import { describe, it, expect, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import "../../lib/i18n";
import { AuthProvider } from "../../auth/AuthProvider";
import { TestQueryProvider } from "../../test/queryClient";
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
  it("timeline renderiza updates mais recente primeiro", async () => {
    await loginAs("owner@vane.app");
    renderDetail("inc-1");
    await screen.findByText(/Identificamos aumento de latência/);

    const bodies = screen.getAllByText(
      /Identificamos aumento de latência|Causa raiz identificada/
    );
    expect(bodies[0]).toHaveTextContent("Causa raiz identificada");
    expect(bodies[1]).toHaveTextContent("Identificamos aumento de latência");
  });

  it("adicionar 2 updates aparece em ordem cronológica reversa", async () => {
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

  it("marcar como resolvido move o incidente pro histórico mantendo a timeline acessível", async () => {
    await loginAs("owner@vane.app");
    renderDetail("inc-1");
    await screen.findByText(/Identificamos aumento de latência/);

    await userEvent.click(screen.getByRole("button", { name: "Marcar como resolvido" }));

    await waitFor(() => expect(screen.getByText("Resolvido")).toBeInTheDocument());
    expect(screen.getByText(/Identificamos aumento de latência/)).toBeInTheDocument();
  });

  it("viewer não vê formulário de update nem botões de transição", async () => {
    await loginAs("viewer@vane.app");
    renderDetail("inc-1");
    await screen.findByText(/Identificamos aumento de latência/);
    expect(screen.queryByLabelText("Novo update")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Marcar como resolvido" })).not.toBeInTheDocument();
  });
});
