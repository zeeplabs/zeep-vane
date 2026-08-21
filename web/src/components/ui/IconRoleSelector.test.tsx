import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { IconRoleSelector } from "./IconRoleSelector";

describe("IconRoleSelector", () => {
  it("marca o papel atual como ativo (accent) e os outros a 40% opacidade", () => {
    render(<IconRoleSelector role="operator" onSelect={() => {}} />);
    const owner = screen.getByRole("button", { name: "Owner" });
    const operator = screen.getByRole("button", { name: "Operator" });
    const viewer = screen.getByRole("button", { name: "Viewer" });

    expect(operator).toHaveAttribute("aria-pressed", "true");
    expect(operator.className).toContain("text-accent");
    expect(owner).toHaveAttribute("aria-pressed", "false");
    expect(owner.className).toContain("opacity-40");
    expect(viewer.className).toContain("opacity-40");
  });

  it("clique dispara onSelect apenas com o papel correspondente ao ícone clicado", async () => {
    const onSelect = vi.fn();
    render(<IconRoleSelector role="viewer" onSelect={onSelect} />);
    await userEvent.click(screen.getByRole("button", { name: "Owner" }));
    expect(onSelect).toHaveBeenCalledWith("owner");
    expect(onSelect).toHaveBeenCalledTimes(1);

    await userEvent.click(screen.getByRole("button", { name: "Operator" }));
    expect(onSelect).toHaveBeenCalledWith("operator");
  });

  it("não exige confirmação embutida — onSelect dispara direto no clique", async () => {
    const onSelect = vi.fn();
    render(<IconRoleSelector role="owner" onSelect={onSelect} />);
    await userEvent.click(screen.getByRole("button", { name: "Viewer" }));
    expect(onSelect).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });
});
