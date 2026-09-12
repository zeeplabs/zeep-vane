import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "../../lib/i18n";
import { LoginTwoFactorStep } from "./LoginTwoFactorStep";

function setup(error: string | null = null) {
  const onSubmit = vi.fn();
  const onBack = vi.fn();
  const onMethodChange = vi.fn();
  render(
    <LoginTwoFactorStep
      submitting={false}
      error={error}
      onSubmit={onSubmit}
      onBack={onBack}
      onMethodChange={onMethodChange}
    />,
  );
  return { onSubmit, onBack, onMethodChange };
}

describe("LoginTwoFactorStep", () => {
  it("submete o código do aplicativo", async () => {
    const { onSubmit } = setup();
    await userEvent.type(screen.getByLabelText("Código de verificação"), "123456");
    await userEvent.click(screen.getByRole("button", { name: "Verificar" }));

    expect(onSubmit).toHaveBeenCalledWith({ code: "123456" });
  });

  it("alternar para recuperação limpa o campo e notifica a página", async () => {
    const { onSubmit, onMethodChange } = setup();
    await userEvent.type(screen.getByLabelText("Código de verificação"), "123456");

    await userEvent.click(screen.getByRole("button", { name: "Usar um código de recuperação" }));

    expect(onMethodChange).toHaveBeenCalledTimes(1);
    expect(screen.getByLabelText("Código de recuperação")).toHaveValue("");

    await userEvent.type(screen.getByLabelText("Código de recuperação"), "REC-0001");
    await userEvent.click(screen.getByRole("button", { name: "Verificar" }));
    expect(onSubmit).toHaveBeenCalledWith({ recoveryCode: "REC-0001" });
  });

  it("alternar de volta para o aplicativo limpa o campo", async () => {
    setup();
    await userEvent.click(screen.getByRole("button", { name: "Usar um código de recuperação" }));
    await userEvent.type(screen.getByLabelText("Código de recuperação"), "REC-0001");

    await userEvent.click(screen.getByRole("button", { name: "Usar o código do aplicativo" }));

    expect(screen.getByLabelText("Código de verificação")).toHaveValue("");
  });

  it("Voltar chama onBack", async () => {
    const { onBack } = setup();
    await userEvent.click(screen.getByRole("button", { name: "Voltar" }));
    expect(onBack).toHaveBeenCalledTimes(1);
  });

  it("mostra o erro inline", () => {
    setup("Código inválido ou expirado. Tente novamente.");
    expect(screen.getByRole("alert")).toHaveTextContent("Código inválido ou expirado. Tente novamente.");
  });
});
