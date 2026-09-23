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
  it("renders the title received via prop", async () => {
    await loginAs("owner@vane.app");
    renderTopbar("Domínios & Status Pages");
    expect(await screen.findByText("Domínios & Status Pages")).toBeInTheDocument();
  });

  it("theme button calls toggleTheme and toggles the data-theme attribute", async () => {
    await loginAs("owner@vane.app");
    renderTopbar("Incidentes");
    const toggle = screen.getByRole("button", { name: "Alternar tema" });

    expect(document.documentElement.dataset.theme).toBeUndefined();
    await userEvent.click(toggle);
    expect(document.documentElement.dataset.theme).toBe("dark");
  });

  it("the bell is static - no href and no onClick", async () => {
    await loginAs("owner@vane.app");
    renderTopbar("Serviços");
    // aria-hidden - not an interactive element, so it has no link/button role.
    expect(screen.queryByRole("link", { name: /notifica/i })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /notifica/i })).not.toBeInTheDocument();
  });

  it("renders the AvatarMenu (user menu trigger)", async () => {
    await loginAs("owner@vane.app");
    renderTopbar("Serviços");
    expect(await screen.findByRole("button", { name: "Menu do usuário" })).toBeInTheDocument();
  });
});
