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
  // PROFPAGE-13: o passo scan renderiza o QR do otpauth_uri e o secret cru.
  it("passo scan mostra o QR e o secret para entrada manual", async () => {
    await loginAsOwner();
    renderDrawer();

    const qr = await screen.findByTestId("enroll-qr");
    expect(qr.querySelector("svg")).not.toBeNull();
    expect(screen.getByTestId("enroll-secret").textContent).toBeTruthy();
  });

  // PROFPAGE-15: código inválido mostra erro inline e mantém o passo verify.
  it("código inválido mostra erro inline e permanece em verify", async () => {
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

  // PROFPAGE-14: código válido avança para o passo codes com os 10 códigos.
  it("código válido mostra os 10 códigos de recuperação uma vez", async () => {
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

  // PROFPAGE-16: concluir o passo codes chama onEnabled (refresh) e fecha.
  it("concluir chama onEnabled e fecha o drawer", async () => {
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

  // spec.md edge case: enroll 409 (já ativado) fecha o drawer, avisa e chama
  // onEnabled em vez de mostrar erro.
  it("enroll 409 fecha o drawer, avisa e chama onEnabled", async () => {
    const onEnabled = vi.fn();
    await loginAsOwner();
    setTwoFactorEnabled(true);
    renderDrawer(onEnabled);

    expect(await screen.findByText("A autenticação em duas etapas já está ativada.")).toBeInTheDocument();
    expect(onEnabled).toHaveBeenCalledTimes(1);
    await waitFor(() => expect(screen.queryByTestId("enroll-qr")).toBeNull());
  });
});
