import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Popover } from "./Popover";

describe("Popover", () => {
  it("fica fechado até o trigger ser clicado", () => {
    render(
      <Popover trigger={<button type="button">abrir</button>}>
        <p>conteúdo do popover</p>
      </Popover>
    );
    expect(screen.queryByText("conteúdo do popover")).not.toBeInTheDocument();
  });

  it("abre ao clicar no trigger", async () => {
    render(
      <Popover trigger={<button type="button">abrir</button>}>
        <p>conteúdo do popover</p>
      </Popover>
    );
    await userEvent.click(screen.getByRole("button", { name: "abrir" }));
    expect(screen.getByText("conteúdo do popover")).toBeInTheDocument();
  });

  it("fecha ao clicar no trigger de novo", async () => {
    render(
      <Popover trigger={<button type="button">abrir</button>}>
        <p>conteúdo do popover</p>
      </Popover>
    );
    const trigger = screen.getByRole("button", { name: "abrir" });
    await userEvent.click(trigger);
    expect(screen.getByText("conteúdo do popover")).toBeInTheDocument();
    await userEvent.click(trigger);
    expect(screen.queryByText("conteúdo do popover")).not.toBeInTheDocument();
  });

  it("fecha ao pressionar Escape", async () => {
    render(
      <Popover trigger={<button type="button">abrir</button>}>
        <p>conteúdo do popover</p>
      </Popover>
    );
    await userEvent.click(screen.getByRole("button", { name: "abrir" }));
    expect(screen.getByText("conteúdo do popover")).toBeInTheDocument();
    await userEvent.keyboard("{Escape}");
    expect(screen.queryByText("conteúdo do popover")).not.toBeInTheDocument();
  });

  it("fecha ao clicar fora", async () => {
    render(
      <div>
        <Popover trigger={<button type="button">abrir</button>}>
          <p>conteúdo do popover</p>
        </Popover>
        <button type="button">fora</button>
      </div>
    );
    await userEvent.click(screen.getByRole("button", { name: "abrir" }));
    expect(screen.getByText("conteúdo do popover")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "fora" }));
    expect(screen.queryByText("conteúdo do popover")).not.toBeInTheDocument();
  });

  it("abre e fecha via teclado (Enter no trigger focado, Escape fecha)", async () => {
    render(
      <Popover trigger={<button type="button">abrir</button>}>
        <p>conteúdo do popover</p>
      </Popover>
    );
    await userEvent.tab();
    expect(screen.getByRole("button", { name: "abrir" })).toHaveFocus();
    await userEvent.keyboard("{Enter}");
    expect(screen.getByText("conteúdo do popover")).toBeInTheDocument();
    await userEvent.keyboard("{Escape}");
    expect(screen.queryByText("conteúdo do popover")).not.toBeInTheDocument();
  });
});
