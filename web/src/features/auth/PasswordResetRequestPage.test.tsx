import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import { PasswordResetRequestPage } from "./PasswordResetRequestPage";

function App() {
  return (
    <MemoryRouter initialEntries={["/reset-password"]}>
      <Routes>
        <Route path="/reset-password" element={<PasswordResetRequestPage />} />
        <Route path="/login" element={<div>login page</div>} />
      </Routes>
    </MemoryRouter>
  );
}

describe("PasswordResetRequestPage", () => {
  it("submeter e-mail mostra confirmação genérica e some com o form", async () => {
    render(<App />);
    await userEvent.type(screen.getByLabelText("E-mail"), "owner@vane.app");
    await userEvent.click(screen.getByRole("button", { name: "Enviar instruções" }));

    expect(
      await screen.findByText(/você receberá instruções para redefinir sua senha/i),
    ).toBeInTheDocument();
    expect(screen.queryByLabelText("E-mail")).not.toBeInTheDocument();
  });

  it("link 'Voltar para o login' navega para /login", async () => {
    render(<App />);
    await userEvent.click(screen.getByRole("link", { name: "Voltar para o login" }));

    expect(await screen.findByText("login page")).toBeInTheDocument();
  });
});
