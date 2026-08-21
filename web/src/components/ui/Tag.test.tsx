import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { Tag } from "./Tag";

describe("Tag", () => {
  it.each(["accent", "accent-outline", "neutral", "neutral-outline"] as const)(
    "renderiza variante %s",
    (variant) => {
      render(<Tag variant={variant}>rótulo</Tag>);
      expect(screen.getByText("rótulo")).toHaveAttribute("data-variant", variant);
    }
  );

  it.each(["success", "warning", "critical"] as const)(
    "variante semântica %s usa color-mix tintado, não fill saturado",
    (variant) => {
      render(<Tag variant={variant}>status</Tag>);
      const el = screen.getByText("status");
      expect(el).toHaveAttribute("data-variant", variant);
      expect(el.style.backgroundColor).toContain("color-mix");
    }
  );
});
