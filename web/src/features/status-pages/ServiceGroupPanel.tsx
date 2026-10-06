import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  DndContext,
  closestCenter,
  KeyboardSensor,
  PointerSensor,
  useDroppable,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import {
  SortableContext,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { MdAdd, MdCheck, MdDragIndicator } from "react-icons/md";
import { Button } from "../../components/ui/Button";
import { Field } from "../../components/ui/Field";
import { ApiError } from "../../lib/apiClient";
import type { Service } from "../../types/api";
import {
  useCreateGroup,
  useSetStatusPageServices,
  useStatusPageGroups,
  useStatusPageServicePlacements,
  type ServicePlacement,
} from "./hooks";

// UNGROUPED is this panel's internal id for the implicit "no group" bucket
// (group_id null/undefined) - never sent to the backend as a real group id
// (ServicePlacement.group_id stays undefined for its members, see
// toServicePlacements), only used as a dnd-kit droppable/container id
// distinct from any real group id (sgroup-* from the backend, T2).
const UNGROUPED = "__ungrouped__";

// PlacementNode is ServiceGroupPanel's client-side working copy of one
// linked service's tree position while the admin drags things around -
// only converted back to the flat, position-ordered ServicePlacement[]
// shape SetServices expects (design.md "position = array index at save
// time") when Save is pressed (toServicePlacements below).
export interface PlacementNode {
  serviceId: string;
  groupId: string | null;
  parentServiceId: string | null;
}

// buildInitialPlacements seeds the panel's working tree from the server's
// per-service placement detail (GET .../services) when it has one, or
// falls back to the flat linked-service order (every service ungrouped,
// unparented) the first time a page's services were only ever set through
// the pre-T13 flow (StatusPageEditorContent.handleSaveServices sends bare
// {service_id, position} placements with no group_id/parent_service_id).
export function buildInitialPlacements(
  linkedServiceIds: string[],
  placements: ServicePlacement[] | undefined
): PlacementNode[] {
  if (placements && placements.length > 0) {
    return [...placements]
      .sort((a, b) => a.position - b.position)
      .map((p) => ({
        serviceId: p.service_id,
        groupId: p.group_id ?? null,
        parentServiceId: p.parent_service_id ?? null,
      }));
  }
  return linkedServiceIds.map((serviceId) => ({ serviceId, groupId: null, parentServiceId: null }));
}

// toServicePlacements flattens the working tree back into the
// position-ordered array SetServices expects: each top-level node (no
// parent) in its container's current order, immediately followed by its
// own children (depth capped at 2 by moveNode below, mirroring the
// backend's validateServicePlacements max-depth rule, SGD-12).
export function toServicePlacements(nodes: PlacementNode[]): ServicePlacement[] {
  const topLevel = nodes.filter((n) => n.parentServiceId === null);
  const out: ServicePlacement[] = [];
  let position = 0;
  for (const top of topLevel) {
    out.push({ service_id: top.serviceId, group_id: top.groupId ?? undefined, position: position++ });
    for (const child of nodes.filter((n) => n.parentServiceId === top.serviceId)) {
      out.push({
        service_id: child.serviceId,
        group_id: child.groupId ?? undefined,
        parent_service_id: child.parentServiceId ?? undefined,
        position: position++,
      });
    }
  }
  return out;
}

// moveNode applies one dnd-kit drag (or keyboard-triggered move) to the
// working tree, pure and independent of dnd-kit's event types so it's
// cheap to unit test directly. overId is one of:
//  - `nest:<serviceId>` - dropped directly onto another top-level service's
//    nest zone (a distinct drop target from the rest of its row, per
//    SGD-07/the Design doc's note) - sets parent_service_id, inherits the
//    target's group.
//  - a container id (UNGROUPED or a real group id) - dropped onto empty
//    space inside a group section - sets group_id, clears any parent.
//  - another service's id - dropped onto/near another top-level row for
//    position reordering (same or cross-container) - inserts just before
//    that row, same group/parent-clearing as above.
// Nesting under a service that already has a parent is refused (depth cap,
// SGD-12): the backend would reject it anyway, this just avoids a round
// trip that's guaranteed to 422.
export function moveNode(
  nodes: PlacementNode[],
  activeId: string,
  overId: string,
  groupIds: string[]
): PlacementNode[] {
  if (activeId === overId) return nodes;
  const active = nodes.find((n) => n.serviceId === activeId);
  if (!active) return nodes;

  if (overId.startsWith("nest:")) {
    const targetId = overId.slice("nest:".length);
    if (targetId === activeId) return nodes;
    const target = nodes.find((n) => n.serviceId === targetId);
    if (!target || target.parentServiceId !== null) return nodes;
    return nodes.map((n) =>
      n.serviceId === activeId ? { ...n, groupId: target.groupId, parentServiceId: targetId } : n
    );
  }

  if (overId === UNGROUPED || groupIds.includes(overId)) {
    const normalizedGroupId = overId === UNGROUPED ? null : overId;
    const moved: PlacementNode = { ...active, groupId: normalizedGroupId, parentServiceId: null };
    return [...nodes.filter((n) => n.serviceId !== activeId), moved];
  }

  const overNode = nodes.find((n) => n.serviceId === overId);
  if (!overNode) return nodes;
  const moved: PlacementNode = { ...active, groupId: overNode.groupId, parentServiceId: null };
  const withoutActive = nodes.filter((n) => n.serviceId !== activeId);
  const insertAt = withoutActive.findIndex((n) => n.serviceId === overId);
  withoutActive.splice(insertAt === -1 ? withoutActive.length : insertAt, 0, moved);
  return withoutActive;
}

export interface ServiceGroupPanelProps {
  statusPageId: string;
  /** Currently-linked services only (the "Linked" side of
   * StatusPageEditorContent's split) - the "Available" side stays a plain
   * checklist, untouched by this component. */
  services: Service[];
  canManage: boolean;
  /** Removes a service from the linked set entirely (same toggle callback
   * StatusPageEditorContent already wires its "Available"/"Linked"
   * checklist through) - kept distinct from this panel's own group/parent/
   * position Save so unlinking still goes through the existing
   * "Save services" flow. */
  onToggle: (serviceId: string) => void;
}

/** Draggable 2-level tree (group -> service, service -> one dependent
 * child) for the "Linked" services of a status page (service-groups-and-
 * dependencies SGD-01/04/07/08/10/11/12). Dragging a service onto a group
 * section sets its group_id; dragging it onto another service's nest zone
 * sets parent_service_id; reordering sets position. Replaces the old
 * ServiceGroup/ServiceRow pair for the Linked list only. */
export function ServiceGroupPanel({ statusPageId, services, canManage, onToggle }: ServiceGroupPanelProps) {
  const { t } = useTranslation();
  const { data: groups } = useStatusPageGroups(statusPageId);
  const { data: placements } = useStatusPageServicePlacements(statusPageId);
  const createGroup = useCreateGroup();
  const setServices = useSetStatusPageServices();

  const linkedServiceIds = useMemo(() => services.map((s) => s.id), [services]);
  // Joining to a primitive string (instead of depending on the `services`
  // array reference, which the parent recreates via .filter() on every
  // render) keeps this resync from firing - and discarding any in-progress
  // drag - on unrelated parent re-renders (e.g. typing in the "Available"
  // search box). Mirrors the intent of StatusPageEditorContent's existing
  // `sameServiceSet` helper, just as an effect-dependency key instead of
  // an equality check.
  const linkedKey = linkedServiceIds.join(",");

  const [nodes, setNodes] = useState<PlacementNode[]>(() => buildInitialPlacements(linkedServiceIds, placements));
  const [newGroupName, setNewGroupName] = useState("");
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setNodes(buildInitialPlacements(linkedServiceIds, placements));
    // linkedServiceIds is intentionally represented by linkedKey here - see
    // comment above.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [linkedKey, placements]);

  const servicesById = useMemo(() => new Map(services.map((s) => [s.id, s])), [services]);
  const groupList = useMemo(() => groups ?? [], [groups]);
  const groupIds = useMemo(() => groupList.map((g) => g.id), [groupList]);

  const sensors = useSensors(
    useSensor(PointerSensor),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates })
  );

  function handleDragEnd(event: DragEndEvent) {
    const { active, over } = event;
    if (!over) return;
    setNodes((prev) => moveNode(prev, String(active.id), String(over.id), groupIds));
  }

  async function handleCreateGroup() {
    const name = newGroupName.trim();
    if (!name) return;
    setError(null);
    try {
      await createGroup.mutateAsync({ statusPageId, name });
      setNewGroupName("");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : t("statusPages.editor.groups.createError"));
    }
  }

  async function handleSavePlacements() {
    setError(null);
    try {
      await setServices.mutateAsync({ id: statusPageId, services: toServicePlacements(nodes) });
    } catch (err) {
      setError(err instanceof ApiError ? err.message : t("statusPages.editor.groups.saveError"));
    }
  }

  function topLevelOf(groupId: string | null): PlacementNode[] {
    return nodes.filter((n) => n.groupId === groupId && n.parentServiceId === null);
  }
  function childrenOf(serviceId: string): PlacementNode[] {
    return nodes.filter((n) => n.parentServiceId === serviceId);
  }

  if (services.length === 0) return null;

  return (
    <div className="flex flex-col gap-3">
      <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={handleDragEnd}>
        <GroupSection
          id={UNGROUPED}
          label={t("statusPages.editor.groups.ungroupedLabel")}
          topLevel={topLevelOf(null)}
          servicesById={servicesById}
          childrenOf={childrenOf}
          canManage={canManage}
          onToggle={onToggle}
        />
        {groupList.map((g) => (
          <GroupSection
            key={g.id}
            id={g.id}
            label={g.name}
            topLevel={topLevelOf(g.id)}
            servicesById={servicesById}
            childrenOf={childrenOf}
            canManage={canManage}
            onToggle={onToggle}
          />
        ))}
      </DndContext>

      {canManage ? (
        <div className="flex items-end gap-2">
          <Field
            type="text"
            variant="filled"
            label={t("statusPages.editor.groups.newGroupLabel")}
            placeholder={t("statusPages.editor.groups.newGroupPlaceholder")}
            value={newGroupName}
            onChange={(e) => setNewGroupName(e.target.value)}
          />
          <Button
            type="button"
            variant="secondary"
            disabled={!newGroupName.trim() || createGroup.isPending}
            onClick={handleCreateGroup}
          >
            <MdAdd size={14} aria-hidden="true" />
            {t("statusPages.editor.groups.createGroupButton")}
          </Button>
        </div>
      ) : null}

      {canManage ? (
        <div className="flex items-center gap-3">
          <Button type="button" variant="solid" disabled={setServices.isPending} onClick={handleSavePlacements}>
            {t("statusPages.editor.groups.saveButton")}
          </Button>
          {error ? (
            <p role="alert" className="text-xs text-critical">
              {error}
            </p>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

interface GroupSectionProps {
  id: string;
  label: string;
  topLevel: PlacementNode[];
  servicesById: Map<string, Service>;
  childrenOf: (serviceId: string) => PlacementNode[];
  canManage: boolean;
  onToggle: (serviceId: string) => void;
}

// GroupSection is one drop target for "assign this service to this group"
// (the whole section, not any particular row within it) - visually
// distinct from a ServiceNode's own nest zone below, per the Design doc's
// requirement that the two drop kinds be told apart.
function GroupSection({ id, label, topLevel, servicesById, childrenOf, canManage, onToggle }: GroupSectionProps) {
  const { t } = useTranslation();
  const { setNodeRef, isOver } = useDroppable({ id });

  return (
    <div className="flex flex-col gap-1">
      <span className="text-[10.5px] font-bold uppercase tracking-wide text-text-muted">{label}</span>
      <div
        ref={setNodeRef}
        className={`flex flex-col gap-0.5 rounded-md border p-1.5 transition-colors ${
          isOver ? "border-accent bg-accent-900/20" : "border-divider"
        }`}
      >
        <SortableContext items={topLevel.map((n) => n.serviceId)} strategy={verticalListSortingStrategy}>
          {topLevel.length === 0 ? (
            <p className="px-2 py-1.5 text-[12.5px] text-text-muted">{t("statusPages.editor.groups.emptyGroup")}</p>
          ) : (
            topLevel.map((node) => {
              const service = servicesById.get(node.serviceId);
              if (!service) return null;
              const childServices = childrenOf(node.serviceId)
                .map((c) => servicesById.get(c.serviceId))
                .filter((s): s is Service => s !== undefined);
              return (
                <ServiceNode
                  key={node.serviceId}
                  service={service}
                  childServices={childServices}
                  canManage={canManage}
                  onToggle={onToggle}
                />
              );
            })
          )}
        </SortableContext>
      </div>
    </div>
  );
}

interface ServiceNodeProps {
  service: Service;
  childServices: Service[];
  canManage: boolean;
  onToggle: (serviceId: string) => void;
}

// ServiceNode is one top-level linked service's row: a drag handle
// (keyboard-operable via dnd-kit's KeyboardSensor, not just pointer/mouse -
// SGD accessibility requirement), the existing toggle-to-remove row
// (ServiceToggleRow, same composition the old ServiceRow had, so it's
// still exactly `getByRole("button", { name: service.name })` for any
// caller/test), any already-nested child rows, and a distinct "drop here
// to nest as a dependent" zone below the whole node.
function ServiceNode({ service, childServices, canManage, onToggle }: ServiceNodeProps) {
  const { t } = useTranslation();
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: service.id });
  const { setNodeRef: setNestRef, isOver: isNestOver } = useDroppable({ id: `nest:${service.id}` });

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
    opacity: isDragging ? 0.5 : 1,
  };

  return (
    <div ref={setNodeRef} style={style} className="flex flex-col gap-0.5">
      <div className="flex items-center gap-1">
        {canManage ? (
          <button
            type="button"
            {...attributes}
            {...listeners}
            aria-label={t("statusPages.editor.groups.dragHandle", { name: service.name })}
            className="flex h-6 w-6 flex-none cursor-grab items-center justify-center text-text-muted active:cursor-grabbing"
          >
            <MdDragIndicator size={16} aria-hidden="true" />
          </button>
        ) : null}
        <ServiceToggleRow name={service.name} disabled={!canManage} onToggle={() => onToggle(service.id)} />
      </div>

      {childServices.map((child) => (
        <div key={child.id} className="ml-7 flex items-center gap-1.5 border-l border-divider pl-2">
          <ServiceToggleRow name={child.name} disabled={!canManage} onToggle={() => onToggle(child.id)} />
        </div>
      ))}

      {canManage ? (
        <div
          ref={setNestRef}
          data-testid={`nest-zone-${service.id}`}
          aria-hidden="true"
          className={`ml-7 h-2 rounded transition-colors ${isNestOver ? "bg-accent/40" : ""}`}
        />
      ) : null}
    </div>
  );
}

interface ServiceToggleRowProps {
  name: string;
  disabled: boolean;
  onToggle: () => void;
}

// ServiceToggleRow is the same checkbox-row composition the removed
// ServiceRow used (16px rounded-square checkbox + MdCheck, aria-pressed
// instead of a native <input type="checkbox">) - kept identical so a
// service's accessible name/role/aria-pressed contract doesn't change for
// any existing caller.
function ServiceToggleRow({ name, disabled, onToggle }: ServiceToggleRowProps) {
  return (
    <button
      type="button"
      onClick={disabled ? undefined : onToggle}
      aria-pressed={true}
      disabled={disabled}
      className="flex min-w-0 flex-1 cursor-pointer items-center gap-2.5 rounded-md px-2.5 py-[9px] text-left hover:bg-card-header-bg disabled:cursor-not-allowed disabled:hover:bg-transparent"
    >
      <span className="flex h-[16px] w-[16px] flex-none items-center justify-center rounded-[5px] border-[1.5px] border-accent bg-accent">
        <MdCheck size={11} className="text-white" aria-hidden="true" />
      </span>
      <span className={`truncate text-[13px] font-semibold ${disabled ? "text-text-muted" : "text-text"}`}>{name}</span>
    </button>
  );
}
