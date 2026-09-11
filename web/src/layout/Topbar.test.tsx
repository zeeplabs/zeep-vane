import { describe, it, expect, afterEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import "../lib/i18n";
import { AuthProvider } from "../auth/AuthProvider";
import { Topbar } from "./Topbar";
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
  document.documentElement.removeAttribute("data-theme");
});

function renderTopbar(title: string) {
  return render(
    <TestQueryProvider>
      <MemoryRouter>
        <AuthProvider>
          <Topbar title={title} />
        </AuthProvider>
      </MemoryRouter>
    </TestQueryProvider>
  );
}

describe("Topbar", () => {
  it("renderiza o título recebido via prop", async () => {
    await loginAs("owner@vane.app");
    renderTopbar("Domínios & Status Pages");
    expect(await screen.findByText("Domínios & Status Pages")).toBeInTheDocument();
  });

  it("botão de tema chama toggleTheme e alterna o atributo data-theme", async () => {
    await loginAs("owner@vane.app");
    renderTopbar("Incidentes");
    const toggle = screen.getByRole("button", { name: "Alternar tema" });

    expect(document.documentElement.dataset.theme).toBeUndefined();
    await userEvent.click(toggle);
    expect(document.documentElement.dataset.theme).toBe("dark");
  });

  it("o sino é estático - sem href e sem onClick", async () => {
    await loginAs("owner@vane.app");
    renderTopbar("Serviços");
    // aria-hidden - não é um elemento interativo, então não tem role de link/botão.
    expect(screen.queryByRole("link", { name: /notifica/i })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /notifica/i })).not.toBeInTheDocument();
  });

  it("renderiza o AvatarMenu (trigger do menu do usuário)", async () => {
    await loginAs("owner@vane.app");
    renderTopbar("Serviços");
    expect(await screen.findByRole("button", { name: "Menu do usuário" })).toBeInTheDocument();
  });
});
