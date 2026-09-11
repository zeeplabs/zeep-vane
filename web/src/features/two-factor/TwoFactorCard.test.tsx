import { describe, it, expect, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Toaster } from "sonner";
import "../../lib/i18n";
import { AuthProvider } from "../../auth/AuthProvider";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch } from "../../lib/apiClient";
import { setTwoFactorEnabled } from "../../test/msw/handlers";
import { TwoFactorCard } from "./TwoFactorCard";

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

afterEach(async () => {
  await apiFetch("/api/auth/logout", { method: "POST" });
});

function renderCard() {
  return render(
    <TestQueryProvider>
      <AuthProvider>
        <Toaster />
        <TwoFactorCard />
      </AuthProvider>
    </TestQueryProvider>
  );
}

describe("TwoFactorCard", () => {
  // PROFPAGE-12: desativado mostra o badge "Desativada" e o botão "Ativar"
  // que abre o drawer de enrollment.
  it("estado desativado mostra Ativar e abre o drawer", async () => {
    const user = userEvent.setup();
    await loginAsOwner();
    renderCard();

    expect(await screen.findByTestId("two-factor-badge")).toHaveTextContent("Desativada");
    await user.click(screen.getByTestId("two-factor-enable"));

    expect(await screen.findByTestId("enroll-qr")).toBeInTheDocument();
  });

  // PROFPAGE-13: ativado mostra o badge "Ativada" e o botão "Desativar" que
  // abre o diálogo.
  it("estado ativado mostra Desativar e abre o diálogo", async () => {
    const user = userEvent.setup();
    await loginAsOwner();
    setTwoFactorEnabled(true);
    renderCard();

    expect(await screen.findByTestId("two-factor-badge")).toHaveTextContent("Ativada");
    await user.click(screen.getByTestId("two-factor-disable"));

    expect(await screen.findByTestId("two-factor-disable-confirm")).toBeInTheDocument();
  });

  // PROFPAGE-17: desativar com sucesso faz o card re-derivar como desativado.
  it("desativar com sucesso faz o card voltar para desativado", async () => {
    const user = userEvent.setup();
    await loginAsOwner();
    setTwoFactorEnabled(true);
    renderCard();

    await screen.findByText("Ativada");
    await user.click(screen.getByTestId("two-factor-disable"));
    await user.type(await screen.findByLabelText("Senha atual"), "demo1234");
    await user.click(screen.getByTestId("two-factor-disable-confirm"));

    await waitFor(() =>
      expect(screen.getByTestId("two-factor-card")).toHaveAttribute("data-enabled", "false")
    );
    expect(screen.getByTestId("two-factor-badge")).toHaveTextContent("Desativada");
  });
});
