import { describe, it, expect, afterEach } from "vitest";
import { render, screen, act } from "@testing-library/react";
import { Toaster } from "sonner";
import "../../lib/i18n";
import i18n from "../../lib/i18n";
import { AuthProvider } from "../../auth/AuthProvider";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch } from "../../lib/apiClient";
import { ProfilePage } from "./ProfilePage";

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

afterEach(async () => {
  await act(async () => {
    await i18n.changeLanguage("pt");
  });
  await apiFetch("/api/auth/logout", { method: "POST" });
});

function renderPage() {
  return render(
    <TestQueryProvider>
      <AuthProvider>
        <Toaster />
        <ProfilePage />
      </AuthProvider>
    </TestQueryProvider>
  );
}

describe("ProfilePage", () => {
  // PROFPAGE-01: a página renderiza o cabeçalho e os três cards.
  it("renderiza o cabeçalho e os três cards", async () => {
    await loginAsOwner();
    renderPage();

    expect(await screen.findByRole("heading", { level: 1, name: "Meu Perfil" })).toBeInTheDocument();
    expect(await screen.findByText("Informações pessoais")).toBeInTheDocument();
    expect(await screen.findByText("Segurança")).toBeInTheDocument();
    expect(await screen.findByText("Sessões ativas")).toBeInTheDocument();
    expect(await screen.findByText("Notificações")).toBeInTheDocument();
  });

  // spec.md edge case: com locale en, todas as strings novas renderizam em
  // inglês.
  it("renderiza as strings em inglês quando o locale é en", async () => {
    await loginAsOwner();
    await act(async () => {
      await i18n.changeLanguage("en");
    });
    renderPage();

    expect(await screen.findByRole("heading", { level: 1, name: "My Profile" })).toBeInTheDocument();
    expect(await screen.findByText("Personal information")).toBeInTheDocument();
    expect(await screen.findByText("Security")).toBeInTheDocument();
    expect(await screen.findByText("Active sessions")).toBeInTheDocument();
    expect(await screen.findByText("Notifications")).toBeInTheDocument();
  });
});
