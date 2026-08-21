import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Seg } from "./Seg";

const options = [
  { value: "a", label: "A" },
  { value: "b", label: "B" },
];

describe("Seg", () => {
  it("marca o item ativo com aria-selected e borda accent", () => {
    render(<Seg options={options} value="a" onChange={() => {}} />);
    const active = screen.getByRole("tab", { name: "A" });
    expect(active).toHaveAttribute("aria-selected", "true");
    expect(active.className).toContain("border-accent");

    const inactive = screen.getByRole("tab", { name: "B" });
    expect(inactive).toHaveAttribute("aria-selected", "false");
  });

  it("dispara onChange com o valor clicado", async () => {
    const onChange = vi.fn();
    render(<Seg options={options} value="a" onChange={onChange} />);
    await userEvent.click(screen.getByRole("tab", { name: "B" }));
    expect(onChange).toHaveBeenCalledWith("b");
  });
});
