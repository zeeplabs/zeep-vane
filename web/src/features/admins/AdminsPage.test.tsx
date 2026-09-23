import { describe, it, expect, afterEach } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { Toaster } from "sonner";
import { http, HttpResponse, delay } from "msw";
import i18n from "../../lib/i18n";
import { AuthProvider } from "../../auth/AuthProvider";
import { TestQueryProvider } from "../../test/queryClient";
import { server } from "../../test/msw/server";
import { apiFetch } from "../../lib/apiClient";
import { seedExpiredAdminInvite } from "../../test/msw/handlers";
import { AdminsPage } from "./AdminsPage";

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
        <AuthProvider>
          <Toaster />
          <AdminsPage />
        </AuthProvider>
      </TestQueryProvider>
    </MemoryRouter>
  );
}

function rowFor(email: string): HTMLElement {
  const match = screen
    .getAllByText(email)
    .map((el) => el.closest('[data-testid="admin-row"]'))
    .find((el): el is HTMLElement => el !== null);
  if (!match) throw new Error(`no admin-row found for ${email}`);
  return match;
}

describe("AdminsPage", () => {
  // SKEL-04/05: while /api/admins is loading, the page shows skeleton rows
  // (not the old "Carregando…" paragraph as visible content) inside an
  // aria-busy container that still carries the sr-only loading string.
  it("shows skeletons (not the text) while /api/admins loads", async () => {
    server.use(
      http.get("/api/admins", async () => {
        await delay("infinite");
        return HttpResponse.json({ items: [], total: 0, page: 1, page_size: 20 });
      }),
    );
    await loginAsOwner();
    renderPage();

    const srText = await screen.findByText("Carregando…");
    expect(srText.className).toContain("sr-only");
    expect(srText.closest('[aria-busy="true"]')).toBeInTheDocument();
    expect(screen.getAllByTestId("skeleton").length).toBeGreaterThan(0);
  });

  // SKEL-06: once the fetch resolves, skeletons are gone and the real table
  // (or its empty state) takes over - loading and loaded are mutually
  // exclusive.
  it("removes the skeletons as soon as /api/admins finishes loading", async () => {
    await loginAsOwner();
    renderPage();

    await screen.findByText("owner@vane.app");
    expect(screen.queryAllByTestId("skeleton")).toHaveLength(0);
  });

  it("lists all users (active and pending) in a single table (USRPG-01)", async () => {
    await loginAsOwner();
    renderPage();

    expect(await screen.findByText("owner@vane.app")).toBeInTheDocument();
    expect(screen.getByText("operator@vane.app")).toBeInTheDocument();
    expect(screen.getByText("viewer@vane.app")).toBeInTheDocument();
    expect(screen.getByText("novo-operador@vane.app")).toBeInTheDocument();
    expect(within(rowFor("novo-operador@vane.app")).getByText("Pendente")).toBeInTheDocument();
  });

  it("role labels are Admin/Membro/Leitura, never owner/operator/viewer (USRPG-01)", async () => {
    await loginAsOwner();
    renderPage();
    await screen.findByText("owner@vane.app");

    expect(within(rowFor("owner@vane.app")).getByText("Admin")).toBeInTheDocument();
    expect(within(rowFor("operator@vane.app")).getByText("Membro")).toBeInTheDocument();
    expect(within(rowFor("viewer@vane.app")).getByText("Leitura")).toBeInTheDocument();
    expect(screen.queryByText("owner", { exact: true })).not.toBeInTheDocument();
  });

  it("role chips filter the table with the correct count (USRPG-02)", async () => {
    await loginAsOwner();
    renderPage();
    await screen.findByText("owner@vane.app");

    await userEvent.click(screen.getByRole("button", { name: /^Admin/ }));

    expect(screen.getByText("owner@vane.app")).toBeInTheDocument();
    expect(screen.queryByText("operator@vane.app")).not.toBeInTheDocument();
    expect(screen.queryByText("viewer@vane.app")).not.toBeInTheDocument();
  });

  it("search filters by name or email (USRPG-03)", async () => {
    await loginAsOwner();
    renderPage();
    await screen.findByText("owner@vane.app");

    await userEvent.type(screen.getByLabelText("Buscar por nome ou email"), "operator");

    expect(screen.getByText("operator@vane.app")).toBeInTheDocument();
    expect(screen.queryByText("owner@vane.app")).not.toBeInTheDocument();
  });

  it("filter with no results shows the empty state (USRPG-04)", async () => {
    await loginAsOwner();
    renderPage();
    await screen.findByText("owner@vane.app");

    await userEvent.type(screen.getByLabelText("Buscar por nome ou email"), "ninguem-existe");

    expect(await screen.findByText("Nenhum usuário encontrado com esses filtros.")).toBeInTheDocument();
  });

  it("clicking a row opens the detail drawer with role and last access (USRPG-06)", async () => {
    await loginAsOwner();
    renderPage();
    await screen.findByText("owner@vane.app");

    await userEvent.click(rowFor("owner@vane.app"));

    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText("owner@vane.app")).toBeInTheDocument();
    expect(within(dialog).getByRole("radio", { name: /Admin/ })).toHaveAttribute("aria-checked", "true");
    expect(within(dialog).getByText(/há \d+/)).toBeInTheDocument();
  });

  it("changing role in the drawer calls the API and reflects in the table (USRPG-07)", async () => {
    await loginAsOwner();
    renderPage();
    await screen.findByText("operator@vane.app");

    await userEvent.click(rowFor("operator@vane.app"));
    const dialog = await screen.findByRole("dialog");
    await userEvent.click(within(dialog).getByRole("radio", { name: /Somente leitura/ }));

    await waitFor(() => {
      expect(within(rowFor("operator@vane.app")).getByText("Leitura")).toBeInTheDocument();
    });
  });

  it("rejected role change (409, last owner) keeps the previous role and shows an error (USRPG-07)", async () => {
    await loginAsOwner();
    renderPage();
    await screen.findByText("owner@vane.app");

    await userEvent.click(rowFor("owner@vane.app"));
    const dialog = await screen.findByRole("dialog");
    await userEvent.click(within(dialog).getByRole("radio", { name: /Membro/ }));

    expect(await within(dialog).findByRole("alert")).toHaveTextContent(/zero active owners/);
    expect(within(rowFor("owner@vane.app")).getByText("Admin")).toBeInTheDocument();
  });

  it("pending invite drawer shows Reenviar convite; active does not (USRPG-08)", async () => {
    await loginAsOwner();
    renderPage();
    await screen.findByText("novo-operador@vane.app");

    await userEvent.click(rowFor("novo-operador@vane.app"));
    let dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByRole("button", { name: "Reenviar convite" })).toBeInTheDocument();
    await userEvent.click(within(dialog).getByLabelText("Fechar"));

    await userEvent.click(rowFor("owner@vane.app"));
    dialog = await screen.findByRole("dialog");
    expect(within(dialog).queryByRole("button", { name: "Reenviar convite" })).not.toBeInTheDocument();
  });

  it("removing an active user from the drawer removes the row and shows a toast (USRPG-09)", async () => {
    await loginAsOwner();
    renderPage();
    await screen.findByText("operator@vane.app");

    await userEvent.click(rowFor("operator@vane.app"));
    const dialog = await screen.findByRole("dialog");
    await userEvent.click(within(dialog).getByRole("button", { name: "Remover usuário" }));

    await userEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Remover" }));

    await waitFor(() => expect(screen.queryByText("operator@vane.app")).not.toBeInTheDocument());
    expect(await screen.findByText("Acesso de operator@vane.app removido.")).toBeInTheDocument();
  });

  it("cancelling a pending invite from the drawer removes the row and shows a toast (USRPG-09)", async () => {
    await loginAsOwner();
    renderPage();
    await screen.findByText("novo-operador@vane.app");

    await userEvent.click(rowFor("novo-operador@vane.app"));
    const dialog = await screen.findByRole("dialog");
    await userEvent.click(within(dialog).getByRole("button", { name: "Remover usuário" }));
    await userEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Remover" }));

    await waitFor(() => expect(screen.queryByText("novo-operador@vane.app")).not.toBeInTheDocument());
    expect(await screen.findByText("Convite de novo-operador@vane.app cancelado.")).toBeInTheDocument();
  });

  it("missing last access shows an em dash (USRPG-10)", async () => {
    await loginAsOwner();
    renderPage();
    await screen.findByText("novo-operador@vane.app");

    expect(within(rowFor("novo-operador@vane.app")).getByText("—")).toBeInTheDocument();
  });

  it("inviting a user via the drawer requires name, optional phone, and role (USRPG-11/12)", async () => {
    await loginAsOwner();
    renderPage();
    await screen.findByText("owner@vane.app");

    await userEvent.click(screen.getByRole("button", { name: "Convidar usuário" }));
    await userEvent.type(screen.getByLabelText("Nome"), "Novo Viewer");
    await userEvent.type(screen.getByLabelText("Email"), "novo-viewer@vane.app");
    await userEvent.click(screen.getByRole("radio", { name: /Somente leitura/ }));
    await userEvent.click(screen.getByRole("button", { name: "Enviar convite" }));

    expect(await screen.findByText("novo-viewer@vane.app")).toBeInTheDocument();
    expect(within(rowFor("novo-viewer@vane.app")).getByText("Pendente")).toBeInTheDocument();
  });

  it("rejected invite (409, email already active) keeps the drawer open and shows the error (USRPG-13)", async () => {
    await loginAsOwner();
    renderPage();
    await screen.findByText("owner@vane.app");

    await userEvent.click(screen.getByRole("button", { name: "Convidar usuário" }));
    await userEvent.type(screen.getByLabelText("Nome"), "Duplicado");
    await userEvent.type(screen.getByLabelText("Email"), "owner@vane.app");
    await userEvent.click(screen.getByRole("button", { name: "Enviar convite" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(/an active admin already exists/);
    expect(screen.getByRole("button", { name: "Enviar convite" })).toBeInTheDocument();
  });

  it("resending a pending invite keeps the row and shows a confirmation toast (INVITE-03)", async () => {
    await loginAsOwner();
    renderPage();
    await screen.findByText("novo-operador@vane.app");

    await userEvent.click(rowFor("novo-operador@vane.app"));
    const dialog = await screen.findByRole("dialog");
    await userEvent.click(within(dialog).getByRole("button", { name: "Reenviar convite" }));

    expect(await screen.findByText("Convite reenviado para novo-operador@vane.app.")).toBeInTheDocument();
  });

  it("expired pending invite shows the Expirado tag alongside Pendente (INVITE-07)", async () => {
    await loginAsOwner();
    seedExpiredAdminInvite("expirado@vane.app", "viewer");
    renderPage();
    await screen.findByText("expirado@vane.app");

    const expiredRow = rowFor("expirado@vane.app");
    expect(within(expiredRow).getByText("Expirado")).toBeInTheDocument();
    expect(within(expiredRow).getByText("Pendente")).toBeInTheDocument();

    const freshRow = rowFor("novo-operador@vane.app");
    expect(within(freshRow).queryByText("Expirado")).not.toBeInTheDocument();
  });

  it("renders in English when the active language is en", async () => {
    await loginAsOwner();
    await i18n.changeLanguage("en");

    try {
      renderPage();

      expect(await screen.findByText("Users")).toBeInTheDocument();
      expect(screen.getByText("Invite user")).toBeInTheDocument();
    } finally {
      await i18n.changeLanguage("pt-BR");
    }
  });
});
