import { describe, it, expect, vi, afterEach } from "vitest";
import { http, HttpResponse } from "msw";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "../../lib/i18n";
import { server } from "../../test/msw/server";
import { TestQueryProvider } from "../../test/queryClient";
import * as apiClient from "../../lib/apiClient";
import type { Service } from "../../types/api";
import {
  ServiceGroupPanel,
  buildInitialPlacements,
  reconcilePlacements,
  reorderGroup,
  toServicePlacements,
  moveNode,
  type PlacementNode,
} from "./ServiceGroupPanel";
import type { ServicePlacement } from "./hooks";
import type { StatusPageServiceGroup } from "../../types/api";

async function loginAsOwner() {
  await apiClient.apiFetch("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: "owner@vane.app", password: "demo1234" }),
  });
}

afterEach(async () => {
  await apiClient.apiFetch("/api/auth/logout", { method: "POST" });
});

const svc1: Service = {
  id: "svc-1",
  name: "API pública",
  slo_id: null,
  slo_name: null,
  slo_type: "metric",
  datadog_service_tag: "",
  monitor_mode: "slo",
  poll_type: null,
  poll_target: null,
  poll_interval_seconds: null,
  environment: "",
  current_status: "operational",
  last_status_change_at: new Date().toISOString(),
  uptime_30d: null,
  last_seen_at: null,
};

const svc2: Service = { ...svc1, id: "svc-2", name: "Checkout" };

function renderPanel(services: Service[], onToggle = vi.fn()) {
  return {
    onToggle,
    ...render(
      <TestQueryProvider>
        <ServiceGroupPanel statusPageId="sp-1" services={services} canManage onToggle={onToggle} />
      </TestQueryProvider>
    ),
  };
}

describe("ServiceGroupPanel", () => {
  it("renders every linked service, checked, inside the Ungrouped section by default", async () => {
    await loginAsOwner();
    renderPanel([svc1, svc2]);

    expect(await screen.findByText("Sem grupo")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "API pública" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("button", { name: "Checkout" })).toHaveAttribute("aria-pressed", "true");
  });

  it("clicking a linked service's row still calls onToggle (unlink), same contract as the old ServiceRow", async () => {
    await loginAsOwner();
    const { onToggle } = renderPanel([svc1, svc2]);

    await userEvent.click(await screen.findByRole("button", { name: "API pública" }));
    expect(onToggle).toHaveBeenCalledWith("svc-1");
  });

  it("wires a keyboard-operable drag handle per service (tabIndex 0, role button) - not pointer-only", async () => {
    await loginAsOwner();
    renderPanel([svc1]);

    const handle = await screen.findByRole("button", { name: "Reordenar API pública" });
    expect(handle).toHaveAttribute("tabindex", "0");
  });

  it("creates a group inline via useCreateGroup and renders a new section for it", async () => {
    await loginAsOwner();
    renderPanel([svc1]);

    await userEvent.type(screen.getByLabelText("Novo grupo"), "Pagamentos");
    await userEvent.click(screen.getByRole("button", { name: "Criar grupo" }));

    expect(await screen.findByText("Pagamentos")).toBeInTheDocument();
  });

  it("Save placement persists the working tree via PATCH .../services (SetServices)", async () => {
    await loginAsOwner();
    renderPanel([svc1, svc2]);

    await userEvent.click(await screen.findByRole("button", { name: "Salvar posicionamento" }));

    await vi.waitFor(async () => {
      const placements = await apiClient.apiFetch<ServicePlacement[]>("/api/status-pages/sp-1/services");
      expect(placements.map((p) => p.service_id)).toEqual(["svc-1", "svc-2"]);
      expect(placements.every((p) => p.group_id === undefined && p.parent_service_id === undefined)).toBe(true);
    });
  });

  it("surfaces a 422 placement-rule violation inline instead of failing silently", async () => {
    await loginAsOwner();
    server.use(
      http.patch("/api/status-pages/:id/services", () =>
        HttpResponse.json({ error: "a service cannot be its own parent" }, { status: 422 })
      )
    );
    renderPanel([svc1, svc2]);

    await userEvent.click(await screen.findByRole("button", { name: "Salvar posicionamento" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("a service cannot be its own parent");
  });

  it("renders nothing when there are no linked services", async () => {
    await loginAsOwner();
    const { container } = renderPanel([]);
    expect(container.firstChild).toBeNull();
  });

  // M3: rename control on a group's header, inline edit same as
  // ServiceDetailDrawer's name edit.
  it("renames a group inline via useUpdateGroup (M3)", async () => {
    await loginAsOwner();
    renderPanel([svc1]);
    await userEvent.type(screen.getByLabelText("Novo grupo"), "Pagamentos");
    await userEvent.click(screen.getByRole("button", { name: "Criar grupo" }));
    expect(await screen.findByText("Pagamentos")).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Renomear Pagamentos" }));
    const input = screen.getByLabelText("Nome do grupo");
    await userEvent.clear(input);
    await userEvent.type(input, "Cobrança");
    await userEvent.click(screen.getByRole("button", { name: "Salvar nome" }));

    expect(await screen.findByText("Cobrança")).toBeInTheDocument();
    expect(screen.queryByText("Pagamentos")).not.toBeInTheDocument();
  });

  // M3: delete-group control with a confirmation, same Dialog pattern
  // ServiceDetailDrawer's delete-service confirmation uses.
  it("deletes a group after confirmation, leaving it ungrouped server-side (M3)", async () => {
    await loginAsOwner();
    renderPanel([svc1]);
    await userEvent.type(screen.getByLabelText("Novo grupo"), "Pagamentos");
    await userEvent.click(screen.getByRole("button", { name: "Criar grupo" }));
    expect(await screen.findByText("Pagamentos")).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Excluir Pagamentos" }));
    expect(await screen.findByText("Excluir grupo?")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Excluir grupo" }));

    await vi.waitFor(() => expect(screen.queryByText("Pagamentos")).not.toBeInTheDocument());
  });

  // M3: group reorder (persists via useUpdateGroup's position field) - see
  // reorderGroup's comment for why this is up/down controls rather than a
  // third dnd-kit id namespace.
  it("reorders groups via the move-up/move-down controls, persisting new positions (M3)", async () => {
    await loginAsOwner();
    renderPanel([svc1]);
    await userEvent.type(screen.getByLabelText("Novo grupo"), "Alpha");
    await userEvent.click(screen.getByRole("button", { name: "Criar grupo" }));
    await screen.findByText("Alpha");
    await userEvent.type(screen.getByLabelText("Novo grupo"), "Beta");
    await userEvent.click(screen.getByRole("button", { name: "Criar grupo" }));
    await screen.findByText("Beta");

    await userEvent.click(screen.getByRole("button", { name: "Mover Beta para cima" }));

    await vi.waitFor(async () => {
      const groups = await apiClient.apiFetch<{ id: string; name: string; position: number }[]>(
        "/api/status-pages/sp-1/groups"
      );
      const beta = groups.find((g) => g.name === "Beta");
      const alpha = groups.find((g) => g.name === "Alpha");
      expect(beta?.position).toBeLessThan(alpha?.position ?? Infinity);
    });
  });
});

describe("buildInitialPlacements", () => {
  it("falls back to a flat, ungrouped/unparented tree when the server has no placement detail yet", () => {
    expect(buildInitialPlacements(["svc-1", "svc-2"], [])).toEqual([
      { serviceId: "svc-1", groupId: null, parentServiceId: null },
      { serviceId: "svc-2", groupId: null, parentServiceId: null },
    ] satisfies PlacementNode[]);
  });

  it("rebuilds the tree from server placement detail, position-ordered", () => {
    const placements: ServicePlacement[] = [
      { service_id: "svc-2", position: 1, group_id: "sgroup-1" },
      { service_id: "svc-1", position: 0 },
    ];
    expect(buildInitialPlacements(["svc-1", "svc-2"], placements)).toEqual([
      { serviceId: "svc-1", groupId: null, parentServiceId: null },
      { serviceId: "svc-2", groupId: "sgroup-1", parentServiceId: null },
    ] satisfies PlacementNode[]);
  });
});

describe("toServicePlacements", () => {
  it("flattens top-level nodes followed immediately by their own children, positions as array index", () => {
    const nodes: PlacementNode[] = [
      { serviceId: "svc-1", groupId: "sgroup-1", parentServiceId: null },
      { serviceId: "svc-3", groupId: "sgroup-1", parentServiceId: "svc-1" },
      { serviceId: "svc-2", groupId: null, parentServiceId: null },
    ];
    expect(toServicePlacements(nodes)).toEqual([
      { service_id: "svc-1", group_id: "sgroup-1", position: 0 },
      { service_id: "svc-3", group_id: "sgroup-1", parent_service_id: "svc-1", position: 1 },
      { service_id: "svc-2", group_id: undefined, position: 2 },
    ]);
  });
});

describe("moveNode", () => {
  const base: PlacementNode[] = [
    { serviceId: "svc-1", groupId: null, parentServiceId: null },
    { serviceId: "svc-2", groupId: null, parentServiceId: null },
  ];

  it("drag onto a group's container id sets group_id and clears any parent", () => {
    const result = moveNode(base, "svc-1", "sgroup-1", ["sgroup-1"]);
    expect(result.find((n) => n.serviceId === "svc-1")).toEqual({
      serviceId: "svc-1",
      groupId: "sgroup-1",
      parentServiceId: null,
    });
  });

  it("drag onto another service's nest zone sets parent_service_id and inherits its group", () => {
    const grouped: PlacementNode[] = [
      { serviceId: "svc-1", groupId: "sgroup-1", parentServiceId: null },
      { serviceId: "svc-2", groupId: null, parentServiceId: null },
    ];
    const result = moveNode(grouped, "svc-2", "nest:svc-1", ["sgroup-1"]);
    expect(result.find((n) => n.serviceId === "svc-2")).toEqual({
      serviceId: "svc-2",
      groupId: "sgroup-1",
      parentServiceId: "svc-1",
    });
  });

  it("refuses to nest under a service that already has a parent (depth cap, SGD-12)", () => {
    const nested: PlacementNode[] = [
      { serviceId: "svc-1", groupId: null, parentServiceId: null },
      { serviceId: "svc-2", groupId: null, parentServiceId: "svc-1" },
      { serviceId: "svc-3", groupId: null, parentServiceId: null },
    ];
    const result = moveNode(nested, "svc-3", "nest:svc-2", []);
    expect(result).toEqual(nested);
  });

  it("reorders within the same container when dropped onto another top-level service", () => {
    const result = moveNode(base, "svc-2", "svc-1", []);
    expect(result.map((n) => n.serviceId)).toEqual(["svc-2", "svc-1"]);
  });

  it("is a no-op when dropped onto itself", () => {
    expect(moveNode(base, "svc-1", "svc-1", [])).toBe(base);
  });

  // M4: moving a parent must bring its children's group_id along (the
  // public page's nestServices only nests within the same group section),
  // while leaving parent_service_id - the dependency relationship itself -
  // untouched.
  it("moving a parent onto a different group also moves its children's group_id, keeping parent_service_id", () => {
    const withChild: PlacementNode[] = [
      { serviceId: "svc-1", groupId: "sgroup-1", parentServiceId: null },
      { serviceId: "svc-2", groupId: "sgroup-1", parentServiceId: "svc-1" },
    ];
    const result = moveNode(withChild, "svc-1", "sgroup-2", ["sgroup-1", "sgroup-2"]);
    expect(result).toEqual(
      expect.arrayContaining([
        { serviceId: "svc-1", groupId: "sgroup-2", parentServiceId: null },
        { serviceId: "svc-2", groupId: "sgroup-2", parentServiceId: "svc-1" },
      ])
    );
  });

  it("moving a parent via reorder-drop onto another top-level service also moves its children's group_id", () => {
    const withChild: PlacementNode[] = [
      { serviceId: "svc-1", groupId: "sgroup-1", parentServiceId: null },
      { serviceId: "svc-2", groupId: "sgroup-1", parentServiceId: "svc-1" },
      { serviceId: "svc-3", groupId: "sgroup-2", parentServiceId: null },
    ];
    const result = moveNode(withChild, "svc-1", "svc-3", ["sgroup-1", "sgroup-2"]);
    expect(result.find((n) => n.serviceId === "svc-2")).toEqual({
      serviceId: "svc-2",
      groupId: "sgroup-2",
      parentServiceId: "svc-1",
    });
  });

  it("refuses to nest a parent-with-children into another service's nest zone (M4 client-side guard)", () => {
    const withChild: PlacementNode[] = [
      { serviceId: "svc-1", groupId: null, parentServiceId: null },
      { serviceId: "svc-2", groupId: null, parentServiceId: "svc-1" },
      { serviceId: "svc-3", groupId: null, parentServiceId: null },
    ];
    const result = moveNode(withChild, "svc-1", "nest:svc-3", []);
    expect(result).toEqual(withChild);
  });
});

describe("reconcilePlacements", () => {
  it("adds a newly-linked service as an unassigned/ungrouped entry (M2a)", () => {
    const nodes: PlacementNode[] = [{ serviceId: "svc-1", groupId: "sgroup-1", parentServiceId: null }];
    const result = reconcilePlacements(nodes, ["svc-1", "svc-2"]);
    expect(result).toEqual(
      expect.arrayContaining([
        { serviceId: "svc-1", groupId: "sgroup-1", parentServiceId: null },
        { serviceId: "svc-2", groupId: null, parentServiceId: null },
      ])
    );
  });

  it("fully removes a newly-unlinked service from the tree, not just from render (M2b)", () => {
    const nodes: PlacementNode[] = [
      { serviceId: "svc-1", groupId: "sgroup-1", parentServiceId: null },
      { serviceId: "svc-2", groupId: null, parentServiceId: null },
    ];
    const result = reconcilePlacements(nodes, ["svc-1"]);
    expect(result).toEqual([{ serviceId: "svc-1", groupId: "sgroup-1", parentServiceId: null }]);
  });

  it("orphans a child whose parent was unlinked, instead of leaving a dangling parent_service_id", () => {
    const nodes: PlacementNode[] = [
      { serviceId: "svc-1", groupId: "sgroup-1", parentServiceId: null },
      { serviceId: "svc-2", groupId: "sgroup-1", parentServiceId: "svc-1" },
    ];
    const result = reconcilePlacements(nodes, ["svc-2"]);
    expect(result).toEqual([{ serviceId: "svc-2", groupId: "sgroup-1", parentServiceId: null }]);
  });
});

describe("reorderGroup", () => {
  const groups: StatusPageServiceGroup[] = [
    { id: "g1", status_page_id: "sp-1", name: "A", position: 0, created_at: "" },
    { id: "g2", status_page_id: "sp-1", name: "B", position: 1, created_at: "" },
    { id: "g3", status_page_id: "sp-1", name: "C", position: 2, created_at: "" },
  ];

  it("swaps positions with the previous group on 'up'", () => {
    expect(reorderGroup(groups, "g2", "up")).toEqual([
      { groupId: "g2", position: 0 },
      { groupId: "g1", position: 1 },
    ]);
  });

  it("swaps positions with the next group on 'down'", () => {
    expect(reorderGroup(groups, "g2", "down")).toEqual([
      { groupId: "g2", position: 2 },
      { groupId: "g3", position: 1 },
    ]);
  });

  it("is a no-op at the top boundary", () => {
    expect(reorderGroup(groups, "g1", "up")).toEqual([]);
  });

  it("is a no-op at the bottom boundary", () => {
    expect(reorderGroup(groups, "g3", "down")).toEqual([]);
  });
});
