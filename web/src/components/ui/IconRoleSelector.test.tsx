import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import i18n from "../../lib/i18n";
import { IconRoleSelector } from "./IconRoleSelector";

describe("IconRoleSelector", () => {
  it("marks the current role as active (accent) and the others at 40% opacity", () => {
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

  it("clicking fires onSelect only with the role matching the clicked icon", async () => {
    const onSelect = vi.fn();
    render(<IconRoleSelector role="viewer" onSelect={onSelect} />);
    await userEvent.click(screen.getByRole("button", { name: "Owner" }));
    expect(onSelect).toHaveBeenCalledWith("owner");
    expect(onSelect).toHaveBeenCalledTimes(1);

    await userEvent.click(screen.getByRole("button", { name: "Operator" }));
    expect(onSelect).toHaveBeenCalledWith("operator");
  });

  it("does not require an inline confirmation — onSelect fires directly on click", async () => {
    const onSelect = vi.fn();
    render(<IconRoleSelector role="owner" onSelect={onSelect} />);
    await userEvent.click(screen.getByRole("button", { name: "Viewer" }));
    expect(onSelect).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("renders in English when the active language is en", async () => {
    await i18n.changeLanguage("en");

    try {
      render(<IconRoleSelector role="owner" onSelect={() => {}} />);
      expect(screen.getByRole("group", { name: "Select role" })).toBeInTheDocument();
    } finally {
      await i18n.changeLanguage("pt-BR");
    }
  });
});
