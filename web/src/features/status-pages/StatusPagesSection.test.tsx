import { describe, it, expect, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import "../../lib/i18n";
import { AuthProvider } from "../../auth/AuthProvider";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch } from "../../lib/apiClient";
import { StatusPagesSection } from "./StatusPagesSection";

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
          <StatusPagesSection />
        </AuthProvider>
      </TestQueryProvider>
    </MemoryRouter>
  );
}

describe("StatusPagesSection", () => {
  it("formulário de criação não tem campos de domínio/subdomínio (SPD-01)", async () => {
    await loginAsOwner();
    renderSection();
    await userEvent.click(await screen.findByRole("button", { name: "Criar status page" }));

    expect(screen.getByLabelText("Nome")).toBeInTheDocument();
    expect(screen.queryByLabelText("Subdomínio")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Domínio")).not.toBeInTheDocument();
  });

  it("linha de uma página sem domínio renderiza sem URL quebrada (sem 'https://null')", async () => {
    await loginAsOwner();
    renderSection();

    await userEvent.click(await screen.findByRole("button", { name: "Criar status page" }));
    await userEvent.type(screen.getByLabelText("Nome"), "Página Sem Domínio Section Test");
    await userEvent.click(screen.getByRole("button", { name: "Criar" }));

    await waitFor(() => expect(screen.queryByLabelText("Nome")).not.toBeInTheDocument());
    expect(await screen.findByText("Página Sem Domínio Section Test")).toBeInTheDocument();
    expect(document.body.textContent).not.toContain("https://null");
    expect(document.body.textContent).not.toContain("undefined");
  });
});
