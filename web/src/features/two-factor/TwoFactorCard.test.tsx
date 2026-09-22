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
  // PROFPAGE-12: disabled shows the "Desativada" badge and the "Ativar"
  // button that opens the enrollment drawer.
  it("disabled state shows Ativar and opens the drawer", async () => {
    const user = userEvent.setup();
    await loginAsOwner();
    renderCard();

    expect(await screen.findByTestId("two-factor-badge")).toHaveTextContent("Desativada");
    await user.click(screen.getByTestId("two-factor-enable"));

    expect(await screen.findByTestId("enroll-qr")).toBeInTheDocument();
  });

  // PROFPAGE-13: enabled shows the "Ativada" badge and the "Desativar"
  // button that opens the dialog.
  it("enabled state shows Desativar and opens the dialog", async () => {
    const user = userEvent.setup();
    await loginAsOwner();
    setTwoFactorEnabled(true);
    renderCard();

    expect(await screen.findByTestId("two-factor-badge")).toHaveTextContent("Ativada");
    await user.click(screen.getByTestId("two-factor-disable"));

    expect(await screen.findByTestId("two-factor-disable-confirm")).toBeInTheDocument();
  });

  // PROFPAGE-17: successfully disabling makes the card re-derive as disabled.
  it("disabling successfully makes the card go back to disabled", async () => {
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
