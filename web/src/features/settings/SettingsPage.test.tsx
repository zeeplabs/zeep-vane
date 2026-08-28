import { describe, it, expect, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { http, HttpResponse } from "msw";
import { server } from "../../test/msw/server";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch } from "../../lib/apiClient";
import { SettingsPage } from "./SettingsPage";

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

afterEach(async () => {
  await apiFetch("/api/auth/logout", { method: "POST" });
});

function renderPage() {
  return render(
    <MemoryRouter>
      <TestQueryProvider>
        <SettingsPage />
      </TestQueryProvider>
    </MemoryRouter>,
  );
}

describe("SettingsPage", () => {
  it("carrega e exibe o nome/e-mail persistidos", async () => {
    await loginAsOwner();
    renderPage();

    expect(await screen.findByDisplayValue("Sua Empresa Ltda.")).toBeInTheDocument();
    expect(screen.getByDisplayValue("contato@suaempresa.com")).toBeInTheDocument();
  });

  it("editar e submeter chama useUpdateCompanySettings apenas com name e contact_email", async () => {
    await loginAsOwner();
    renderPage();
    await screen.findByDisplayValue("Sua Empresa Ltda.");

    const nameInput = screen.getByLabelText("Nome da empresa");
    await userEvent.clear(nameInput);
    await userEvent.type(nameInput, "Empresa Editada");
    const emailInput = screen.getByLabelText("E-mail de contato");
    await userEvent.clear(emailInput);
    await userEvent.type(emailInput, "editado@empresa.com");

    await userEvent.click(screen.getByRole("button", { name: "Salvar alterações" }));

    await waitFor(() => expect(screen.queryByRole("alert")).not.toBeInTheDocument());

    const persisted = await apiFetch<{ name: string; contact_email: string }>("/api/company-settings");
    expect(persisted.name).toBe("Empresa Editada");
    expect(persisted.contact_email).toBe("editado@empresa.com");
  });

  it("selecionar um arquivo de logo dispara o upload imediatamente, sem depender do submit do form", async () => {
    await loginAsOwner();
    renderPage();
    await screen.findByDisplayValue("Sua Empresa Ltda.");

    const file = new File(["fake-png-bytes"], "logo.png", { type: "image/png" });
    const fileInput = document.querySelector('input[type="file"]') as HTMLInputElement;
    await userEvent.upload(fileInput, file);

    await waitFor(async () => {
      const updated = await apiFetch<{ logo_url: string | null }>("/api/company-settings");
      expect(updated.logo_url).toBe("/uploads/logo");
    });

    // The name/e-mail form was never submitted - only the logo upload ran.
    const persisted = await apiFetch<{ name: string }>("/api/company-settings");
    expect(persisted.name).toBe("Sua Empresa Ltda.");
  });

  it("falha de upload (422) exibe o erro inline existente", async () => {
    server.use(
      http.post(
        "/api/company-settings/logo",
        () =>
          HttpResponse.json({ error: "logo must be a PNG or SVG image no larger than 10 MB" }, { status: 422 }),
      ),
    );
    await loginAsOwner();
    renderPage();
    await screen.findByDisplayValue("Sua Empresa Ltda.");

    const file = new File(["fake-png-bytes"], "logo.png", { type: "image/png" });
    const fileInput = document.querySelector('input[type="file"]') as HTMLInputElement;
    await userEvent.upload(fileInput, file);

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "logo must be a PNG or SVG image no larger than 10 MB",
    );
  });
});
