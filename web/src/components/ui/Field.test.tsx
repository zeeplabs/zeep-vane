import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { Field } from "./Field";

describe("Field", () => {
  it("label acima do campo, associada via htmlFor/id", () => {
    render(<Field label="E-mail" />);
    const input = screen.getByLabelText("E-mail");
    expect(input).toBeInTheDocument();
  });

  it("mostra mensagem de erro quando fornecida", () => {
    render(<Field label="E-mail" error="Campo obrigatório" />);
    expect(screen.getByRole("alert")).toHaveTextContent("Campo obrigatório");
  });

  it("mostra hint quando não há erro", () => {
    render(<Field label="E-mail" hint="Usaremos apenas para contato" />);
    expect(screen.getByText("Usaremos apenas para contato")).toBeInTheDocument();
  });
});
