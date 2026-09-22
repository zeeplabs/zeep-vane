import { describe, it, expect, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import "../lib/i18n";
import { AuthProvider } from "../auth/AuthProvider";
import { AvatarMenu } from "./AvatarMenu";
import { apiFetch } from "../lib/apiClient";
import { TestQueryProvider } from "../test/queryClient";

async function loginAs(email: string) {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email, password: "demo1234" }),
  });
}

afterEach(async () => {
  try {
    await apiFetch("/api/auth/logout", { method: "POST" });
  } catch {
    /* ignore */
  }
});

function renderAvatarMenu() {
  return render(
    <TestQueryProvider>
      <MemoryRouter>
        <AuthProvider>
          <AvatarMenu />
        </AuthProvider>
      </MemoryRouter>
    </TestQueryProvider>
  );
}

describe("AvatarMenu", () => {
  it("shows the authenticated admin's name and email", async () => {
    await loginAs("owner@vane.app");
    renderAvatarMenu();
    await userEvent.click(await screen.findByRole("button", { name: "Menu do usuário" }));

    expect(screen.getByText("Ana Owner")).toBeInTheDocument();
    expect(screen.getByText("owner@vane.app")).toBeInTheDocument();
  });

  it("shows 'Configurações' for owner", async () => {
    await loginAs("owner@vane.app");
    renderAvatarMenu();
    await userEvent.click(await screen.findByRole("button", { name: "Menu do usuário" }));

    expect(screen.getByRole("menuitem", { name: "Configurações" })).toHaveAttribute("href", "/settings");
  });

  it("hides 'Configurações' for non-owner", async () => {
    await loginAs("viewer@vane.app");
    renderAvatarMenu();
    await userEvent.click(await screen.findByRole("button", { name: "Menu do usuário" }));

    expect(screen.queryByRole("menuitem", { name: "Configurações" })).not.toBeInTheDocument();
  });

  // PROFPAGE-02: the "Meu Perfil" link points to /profile for any role.
  it("shows 'Meu Perfil' linking to /profile for owner", async () => {
    await loginAs("owner@vane.app");
    renderAvatarMenu();
    await userEvent.click(await screen.findByRole("button", { name: "Menu do usuário" }));

    expect(screen.getByRole("menuitem", { name: "Meu Perfil" })).toHaveAttribute("href", "/profile");
  });

  it("shows 'Meu Perfil' for non-owner (any role)", async () => {
    await loginAs("viewer@vane.app");
    renderAvatarMenu();
    await userEvent.click(await screen.findByRole("button", { name: "Menu do usuário" }));

    expect(screen.getByRole("menuitem", { name: "Meu Perfil" })).toHaveAttribute("href", "/profile");
  });

  it("'Sair' opens the LogoutConfirmDialog and confirming calls logout", async () => {
    await loginAs("owner@vane.app");
    renderAvatarMenu();
    await userEvent.click(await screen.findByRole("button", { name: "Menu do usuário" }));
    await userEvent.click(screen.getByRole("menuitem", { name: "Sair" }));

    expect(screen.getByText("Sair do painel")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Sair" }));

    // A successful logout() brings admin back to null - the menu button
    // disappears (guard `if (!admin) return null`).
    await waitFor(() => expect(screen.queryByRole("button", { name: "Menu do usuário" })).not.toBeInTheDocument());
  });
});
