import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { MdOutlineExpandMore } from "react-icons/md";
import { useAuth } from "../auth/AuthProvider";
import type { TenantMembership } from "../types/api";

function initialsFor(label: string): string {
  return label.trim().slice(0, 2).toUpperCase();
}

function planLabel(planTier: string, t: (key: string) => string): string {
  return planTier ? planTier : t("tenantSwitcher.freePlan");
}

function displayNameFor(membership: TenantMembership): string {
  // Edge Case (spec.md): a membership missing `name` falls back to
  // `tenant_id` instead of showing a blank row.
  return membership.name || membership.tenant_id;
}

// PlanBadge mirrors the handoff's flat (borderless) plan pill exactly -
// free is a neutral tint, any paid tier is an accent tint. Not the shared
// <Tag> component: Tag's variants are all outlined/dark-filled chips, none
// of which match this specific flat style.
function PlanBadge({ planTier, t }: { planTier: string; t: (key: string) => string }) {
  const isFree = !planTier;
  return (
    <span
      className="shrink-0 rounded-[5px] px-1.5 py-0.5 text-[10.5px] font-bold tracking-wide"
      style={{
        background: isFree ? "var(--color-sidebar-hover-bg)" : "color-mix(in srgb, var(--color-accent) 10%, transparent)",
        color: isFree ? "var(--color-text-muted)" : "var(--color-accent)",
      }}
    >
      {planLabel(planTier, t)}
    </span>
  );
}

// TenantSwitcher is the sidebar's tenant identity block (new-layout-
// migration, SHELL-10/SHELL-11) - distinct from TenantSelector.tsx's
// full-page picker, which only ever shows before an active tenant is
// chosen. Always renders the active tenant's identity (avatar, name, plan
// badge), matching handoff-new-layout/Visao Geral.dc.html; only the
// dropdown/chevron are gated on having more than one membership - the
// every-day single-membership case still shows the identity row, just not
// interactive.
export function TenantSwitcher({ expanded }: { expanded: boolean }) {
  const { t } = useTranslation();
  const { admin, switchTenant } = useAuth();
  const [open, setOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    function handlePointerDown(event: MouseEvent) {
      if (containerRef.current && !containerRef.current.contains(event.target as Node)) {
        setOpen(false);
      }
    }
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") setOpen(false);
    }
    document.addEventListener("mousedown", handlePointerDown);
    document.addEventListener("keydown", handleKeyDown);
    return () => {
      document.removeEventListener("mousedown", handlePointerDown);
      document.removeEventListener("keydown", handleKeyDown);
    };
  }, [open]);

  const memberships = admin?.memberships ?? [];
  if (memberships.length === 0) return null;

  const isMulti = memberships.length > 1;
  const active = memberships.find((m) => m.tenant_id === admin?.active_tenant_id) ?? memberships[0];

  async function handleSelect(tenantId: string) {
    setOpen(false);
    if (tenantId === active.tenant_id) return;
    await switchTenant(tenantId);
  }

  // The name/badge/chevron block fades+grows in sync with the sidebar's own
  // width transition (Sidebar.tsx's labelClass uses the same pattern)
  // instead of mounting/unmounting outright, which popped in with no
  // animation at all.
  const identity = (
    <>
      <span className="flex h-[28px] w-[28px] shrink-0 items-center justify-center rounded-[8px] bg-accent text-[11px] font-semibold text-white">
        {initialsFor(displayNameFor(active))}
      </span>
      <span
        className={
          "flex min-w-0 items-center gap-2 overflow-hidden whitespace-nowrap transition-[opacity,max-width] duration-200 ease-out " +
          (expanded ? "max-w-[180px] flex-1 opacity-100" : "max-w-0 opacity-0")
        }
      >
        <span className="min-w-0 flex-1">
          <span className="block truncate text-[13px] font-medium text-text">{displayNameFor(active)}</span>
        </span>
        <PlanBadge planTier={active.plan_tier} t={t} />
        {isMulti ? (
          <span aria-hidden="true" className="shrink-0 text-text-muted">
            <MdOutlineExpandMore size={14} />
          </span>
        ) : null}
      </span>
    </>
  );

  const rowClass =
    "flex w-full items-center rounded-[10px] px-2 py-1.5 transition-[gap] duration-200 ease-out " +
    (expanded ? "justify-start gap-2" : "justify-center gap-0");

  if (!isMulti) {
    return <div className={rowClass}>{identity}</div>;
  }

  return (
    <div ref={containerRef} className="relative">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-haspopup="listbox"
        aria-expanded={open}
        className={rowClass + " cursor-pointer text-left hover:bg-sidebar-hover-bg"}
      >
        {identity}
      </button>

      {open ? (
        <ul
          role="listbox"
          aria-label={t("tenantSwitcher.label")}
          className="absolute left-0 top-full z-10 mt-1 w-full min-w-[220px] rounded-md border border-divider bg-surface py-1 shadow-sm"
        >
          {memberships.map((m) => {
            const isActive = m.tenant_id === active.tenant_id;
            return (
              <li key={m.tenant_id}>
                <button
                  type="button"
                  role="option"
                  aria-selected={isActive}
                  onClick={() => handleSelect(m.tenant_id)}
                  className="flex w-full cursor-pointer items-center gap-2 px-3 py-1.5 text-left text-[13px] text-text hover:bg-sidebar-hover-bg"
                >
                  <span className="flex h-[24px] w-[24px] shrink-0 items-center justify-center rounded-[7px] bg-accent text-[10px] font-semibold text-white">
                    {initialsFor(displayNameFor(m))}
                  </span>
                  <span className="min-w-0 flex-1 truncate">{displayNameFor(m)}</span>
                  <PlanBadge planTier={m.plan_tier} t={t} />
                  {isActive ? <span aria-hidden="true">✓</span> : null}
                </button>
              </li>
            );
          })}
        </ul>
      ) : null}
    </div>
  );
}
