import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { Field } from "./Field";

describe("Field", () => {
  it("label above the field, associated via htmlFor/id", () => {
    render(<Field label="E-mail" />);
    const input = screen.getByLabelText("E-mail");
    expect(input).toBeInTheDocument();
  });

  it("shows the error message when provided", () => {
    render(<Field label="E-mail" error="Campo obrigatório" />);
    expect(screen.getByRole("alert")).toHaveTextContent("Campo obrigatório");
  });

  it("shows the hint when there's no error", () => {
    render(<Field label="E-mail" hint="Usaremos apenas para contato" />);
    expect(screen.getByText("Usaremos apenas para contato")).toBeInTheDocument();
  });
});
