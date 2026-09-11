import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useAuth } from "../auth/AuthProvider";
import { Tag } from "../components/ui/Tag";
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

// TenantSwitcher is the sidebar's tenant-switcher popover (new-layout-
// migration, SHELL-10/SHELL-11) - distinct from TenantSelector.tsx's
// full-page picker, which only ever shows before an active tenant is
// chosen. Renders null outright for the every-day single-membership case.
export function TenantSwitcher() {
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
  if (memberships.length <= 1) return null;

  const active = memberships.find((m) => m.tenant_id === admin?.active_tenant_id) ?? memberships[0];

  async function handleSelect(tenantId: string) {
    setOpen(false);
    if (tenantId === active.tenant_id) return;
    await switchTenant(tenantId);
  }

  return (
    <div ref={containerRef} className="relative">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-haspopup="listbox"
        aria-expanded={open}
        className="flex w-full cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-left hover:bg-sidebar-hover-bg"
      >
        <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md bg-accent text-[11px] font-semibold text-white">
          {initialsFor(displayNameFor(active))}
        </span>
        <span className="min-w-0 flex-1">
          <span className="block truncate text-[13px] font-medium text-text">{displayNameFor(active)}</span>
        </span>
        <Tag variant={active.plan_tier ? "accent-outline" : "neutral-outline"}>
          {planLabel(active.plan_tier, t)}
        </Tag>
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
                  <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md bg-accent text-[10px] font-semibold text-white">
                    {initialsFor(displayNameFor(m))}
                  </span>
                  <span className="min-w-0 flex-1 truncate">{displayNameFor(m)}</span>
                  <Tag variant={m.plan_tier ? "accent-outline" : "neutral-outline"}>
                    {planLabel(m.plan_tier, t)}
                  </Tag>
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
