import { describe, it, expect, afterEach, vi } from "vitest";
import { useState } from "react";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Toaster } from "sonner";
import "../../lib/i18n";
import { TestQueryProvider } from "../../test/queryClient";
import { apiFetch } from "../../lib/apiClient";
import { mswValidTotpCode, setTwoFactorEnabled } from "../../test/msw/handlers";
import { EnrollDrawer } from "./EnrollDrawer";

async function loginAsOwner() {
  await apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

afterEach(async () => {
  await apiFetch("/api/auth/logout", { method: "POST" });
});

function renderDrawer(onEnabled: () => void = () => {}) {
  function Harness() {
    const [open, setOpen] = useState(true);
    return <EnrollDrawer open={open} onOpenChange={setOpen} onEnabled={onEnabled} />;
  }
  return render(
    <TestQueryProvider>
      <Toaster />
      <Harness />
    </TestQueryProvider>
  );
}

describe("EnrollDrawer", () => {
  // PROFPAGE-13: the scan step renders the otpauth_uri QR and the raw secret.
  it("scan step shows the QR and the secret for manual entry", async () => {
    await loginAsOwner();
    renderDrawer();

    const qr = await screen.findByTestId("enroll-qr");
    expect(qr.querySelector("svg")).not.toBeNull();
    expect(screen.getByTestId("enroll-secret").textContent).toBeTruthy();
  });

  // PROFPAGE-15: invalid code shows an inline error and stays on the verify step.
  it("invalid code shows an inline error and stays on verify", async () => {
    const user = userEvent.setup();
    await loginAsOwner();
    renderDrawer();

    await screen.findByTestId("enroll-qr");
    await user.click(screen.getByRole("button", { name: "Continuar" }));

    const codeInput = screen.getByLabelText("Código de verificação");
    await user.type(codeInput, "000000");
    await user.click(screen.getByRole("button", { name: "Ativar" }));

    expect(await screen.findByText("Código inválido. Tente novamente.")).toBeInTheDocument();
    expect(screen.getByLabelText("Código de verificação")).toBeInTheDocument();
  });

  // PROFPAGE-14: valid code advances to the codes step with the 10 codes.
  it("valid code shows the 10 recovery codes once", async () => {
    const user = userEvent.setup();
    await loginAsOwner();
    renderDrawer();

    await screen.findByTestId("enroll-qr");
    await user.click(screen.getByRole("button", { name: "Continuar" }));
    await user.type(screen.getByLabelText("Código de verificação"), mswValidTotpCode);
    await user.click(screen.getByRole("button", { name: "Ativar" }));

    expect(await screen.findByTestId("recovery-codes")).toBeInTheDocument();
    expect(screen.getAllByTestId("recovery-code")).toHaveLength(10);
    expect(screen.getByText("Estes códigos não serão exibidos novamente.")).toBeInTheDocument();
  });

  // PROFPAGE-16: finishing the codes step calls onEnabled (refresh) and closes.
  it("finishing calls onEnabled and closes the drawer", async () => {
    const user = userEvent.setup();
    const onEnabled = vi.fn();
    await loginAsOwner();
    renderDrawer(onEnabled);

    await screen.findByTestId("enroll-qr");
    await user.click(screen.getByRole("button", { name: "Continuar" }));
    await user.type(screen.getByLabelText("Código de verificação"), mswValidTotpCode);
    await user.click(screen.getByRole("button", { name: "Ativar" }));
    await screen.findByTestId("recovery-codes");

    await user.click(screen.getByRole("button", { name: "Concluir" }));

    expect(onEnabled).toHaveBeenCalledTimes(1);
    await waitFor(() => expect(screen.queryByTestId("recovery-codes")).toBeNull());
  });

  // spec.md edge case: enroll 409 (already enabled) closes the drawer,
  // warns, and calls onEnabled instead of showing an error.
  it("enroll 409 closes the drawer, warns, and calls onEnabled", async () => {
    const onEnabled = vi.fn();
    await loginAsOwner();
    setTwoFactorEnabled(true);
    renderDrawer(onEnabled);

    expect(await screen.findByText("A autenticação em duas etapas já está ativada.")).toBeInTheDocument();
    expect(onEnabled).toHaveBeenCalledTimes(1);
    await waitFor(() => expect(screen.queryByTestId("enroll-qr")).toBeNull());
  });
});
