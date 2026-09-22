import { describe, it, expect, afterEach, vi } from "vitest";
import { useState } from "react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Toaster } from "sonner";
import "../../lib/i18n";
import { AuthProvider } from "../../auth/AuthProvider";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch } from "../../lib/apiClient";
import { setTwoFactorEnabled } from "../../test/msw/handlers";
import { DisableDialog } from "./DisableDialog";

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

afterEach(async () => {
  await apiFetch("/api/auth/logout", { method: "POST" });
});

function renderDialog(onDisabled: () => void = () => {}) {
  function Harness() {
    const [open, setOpen] = useState(true);
    return <DisableDialog open={open} onOpenChange={setOpen} onDisabled={onDisabled} />;
  }
  return render(
    <TestQueryProvider>
      <AuthProvider>
        <Toaster />
        <Harness />
      </AuthProvider>
    </TestQueryProvider>
  );
}

describe("DisableDialog", () => {
  // PROFPAGE-17: correct password disables and calls onDisabled.
  it("correct password disables and calls onDisabled", async () => {
    const user = userEvent.setup();
    const onDisabled = vi.fn();
    await loginAsOwner();
    setTwoFactorEnabled(true);
    renderDialog(onDisabled);

    await user.type(screen.getByLabelText("Senha atual"), "demo1234");
    await user.click(screen.getByTestId("two-factor-disable-confirm"));

    await screen.findByText("Autenticação em duas etapas desativada.");
    expect(onDisabled).toHaveBeenCalledTimes(1);
  });

  // PROFPAGE-18: wrong password shows an inline error and does NOT disable.
  it("wrong password shows an inline error and does not call onDisabled", async () => {
    const user = userEvent.setup();
    const onDisabled = vi.fn();
    await loginAsOwner();
    setTwoFactorEnabled(true);
    renderDialog(onDisabled);

    await user.type(screen.getByLabelText("Senha atual"), "errada");
    await user.click(screen.getByTestId("two-factor-disable-confirm"));

    expect(await screen.findByText("Senha incorreta.")).toBeInTheDocument();
    expect(onDisabled).not.toHaveBeenCalled();
  });
});
