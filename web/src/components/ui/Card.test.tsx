import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { Card } from "./Card";

describe("Card", () => {
  it("usa fundo surface e radius md por padrão", () => {
    render(<Card data-testid="card">conteúdo</Card>);
    const card = screen.getByTestId("card");
    expect(card.className).toContain("bg-surface");
    expect(card.className).toContain("rounded-md");
  });

  it.each(["elev-sm", "elev-md", "elev-lg"] as const)(
    "aplica variante de elevação %s",
    (elevation) => {
      render(
        <Card elevation={elevation} data-testid="card">
          conteúdo
        </Card>
      );
      expect(screen.getByTestId("card")).toHaveAttribute("data-elevation", elevation);
    }
  );
});
