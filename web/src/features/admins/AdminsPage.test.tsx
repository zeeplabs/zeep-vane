import { describe, it, expect, afterEach } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { Toaster } from "sonner";
import { http, HttpResponse, delay } from "msw";
import "../../lib/i18n";
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
  it("mostra skeletons (não o texto) enquanto /api/admins carrega", async () => {
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
  it("remove os skeletons assim que /api/admins termina de carregar", async () => {
    await loginAsOwner();
    renderPage();

    await screen.findByText("owner@vane.app");
    expect(screen.queryAllByTestId("skeleton")).toHaveLength(0);
  });

  it("lista todos os usuários (ativos e pendentes) numa única tabela (USRPG-01)", async () => {
    await loginAsOwner();
    renderPage();

    expect(await screen.findByText("owner@vane.app")).toBeInTheDocument();
    expect(screen.getByText("operator@vane.app")).toBeInTheDocument();
    expect(screen.getByText("viewer@vane.app")).toBeInTheDocument();
    expect(screen.getByText("novo-operador@vane.app")).toBeInTheDocument();
    expect(within(rowFor("novo-operador@vane.app")).getByText("Pendente")).toBeInTheDocument();
  });

  it("rótulos de papel são Admin/Membro/Leitura, nunca owner/operator/viewer (USRPG-01)", async () => {
    await loginAsOwner();
    renderPage();
    await screen.findByText("owner@vane.app");

    expect(within(rowFor("owner@vane.app")).getByText("Admin")).toBeInTheDocument();
    expect(within(rowFor("operator@vane.app")).getByText("Membro")).toBeInTheDocument();
    expect(within(rowFor("viewer@vane.app")).getByText("Leitura")).toBeInTheDocument();
    expect(screen.queryByText("owner", { exact: true })).not.toBeInTheDocument();
  });

  it("chips de papel filtram a tabela com contagem correta (USRPG-02)", async () => {
    await loginAsOwner();
    renderPage();
    await screen.findByText("owner@vane.app");

    await userEvent.click(screen.getByRole("button", { name: /^Admin/ }));

    expect(screen.getByText("owner@vane.app")).toBeInTheDocument();
    expect(screen.queryByText("operator@vane.app")).not.toBeInTheDocument();
    expect(screen.queryByText("viewer@vane.app")).not.toBeInTheDocument();
  });

  it("busca filtra por nome ou e-mail (USRPG-03)", async () => {
    await loginAsOwner();
    renderPage();
    await screen.findByText("owner@vane.app");

    await userEvent.type(screen.getByLabelText("Buscar por nome ou email"), "operator");

    expect(screen.getByText("operator@vane.app")).toBeInTheDocument();
    expect(screen.queryByText("owner@vane.app")).not.toBeInTheDocument();
  });

  it("filtro sem resultado mostra o estado vazio (USRPG-04)", async () => {
    await loginAsOwner();
    renderPage();
    await screen.findByText("owner@vane.app");

    await userEvent.type(screen.getByLabelText("Buscar por nome ou email"), "ninguem-existe");

    expect(await screen.findByText("Nenhum usuário encontrado com esses filtros.")).toBeInTheDocument();
  });

  it("clicar numa linha abre o drawer de detalhe com papel e último acesso (USRPG-06)", async () => {
    await loginAsOwner();
    renderPage();
    await screen.findByText("owner@vane.app");

    await userEvent.click(rowFor("owner@vane.app"));

    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText("owner@vane.app")).toBeInTheDocument();
    expect(within(dialog).getByRole("radio", { name: /Admin/ })).toHaveAttribute("aria-checked", "true");
    expect(within(dialog).getByText(/há \d+/)).toBeInTheDocument();
  });

  it("trocar papel no drawer chama a API e reflete na tabela (USRPG-07)", async () => {
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

  it("troca de papel rejeitada (409, último owner) mantém o papel anterior e mostra erro (USRPG-07)", async () => {
    await loginAsOwner();
    renderPage();
    await screen.findByText("owner@vane.app");

    await userEvent.click(rowFor("owner@vane.app"));
    const dialog = await screen.findByRole("dialog");
    await userEvent.click(within(dialog).getByRole("radio", { name: /Membro/ }));

    expect(await within(dialog).findByRole("alert")).toHaveTextContent(/zero active owners/);
    expect(within(rowFor("owner@vane.app")).getByText("Admin")).toBeInTheDocument();
  });

  it("drawer de convite pendente mostra Reenviar convite; ativo não mostra (USRPG-08)", async () => {
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

  it("remover usuário ativo pelo drawer remove a linha e mostra toast (USRPG-09)", async () => {
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

  it("cancelar convite pendente pelo drawer remove a linha e mostra toast (USRPG-09)", async () => {
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

  it("último acesso ausente mostra travessão (USRPG-10)", async () => {
    await loginAsOwner();
    renderPage();
    await screen.findByText("novo-operador@vane.app");

    expect(within(rowFor("novo-operador@vane.app")).getByText("—")).toBeInTheDocument();
  });

  it("convidar usuário via drawer exige nome, telefone opcional e papel (USRPG-11/12)", async () => {
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

  it("convite rejeitado (409, e-mail já ativo) mantém o drawer aberto e mostra o erro (USRPG-13)", async () => {
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

  it("reenviar convite pendente mantém a linha e exibe toast de confirmação (INVITE-03)", async () => {
    await loginAsOwner();
    renderPage();
    await screen.findByText("novo-operador@vane.app");

    await userEvent.click(rowFor("novo-operador@vane.app"));
    const dialog = await screen.findByRole("dialog");
    await userEvent.click(within(dialog).getByRole("button", { name: "Reenviar convite" }));

    expect(await screen.findByText("Convite reenviado para novo-operador@vane.app.")).toBeInTheDocument();
  });

  it("convite pendente expirado exibe tag Expirado além de Pendente (INVITE-07)", async () => {
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
});
