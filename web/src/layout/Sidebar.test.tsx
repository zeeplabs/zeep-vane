import { describe, it, expect, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import "../lib/i18n";
import { AuthProvider } from "../auth/AuthProvider";
import { Sidebar } from "./Sidebar";
import { apiFetch } from "../lib/apiClient";

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

function renderSidebar() {
  return render(
    <MemoryRouter>
      <AuthProvider>
        <Sidebar />
      </AuthProvider>
    </MemoryRouter>
  );
}

describe("Sidebar", () => {
  it("esconde 'Admins' para non-owner", async () => {
    await loginAs("viewer@vane.app");
    renderSidebar();
    await waitFor(() => expect(screen.getByText("Domínios & Status Pages")).toBeInTheDocument());
    expect(screen.queryByText("Admins")).not.toBeInTheDocument();
  });

  it("mostra 'Admins' para owner", async () => {
    await loginAs("owner@vane.app");
    renderSidebar();
    await waitFor(() => expect(screen.getByText("Admins")).toBeInTheDocument());
  });
});
