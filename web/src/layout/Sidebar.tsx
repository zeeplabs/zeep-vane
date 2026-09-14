import { useState } from "react";
import { NavLink, useLocation, useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import {
  MdOutlineCable,
  MdOutlineGridView,
  MdOutlinePublic,
  MdOutlineWarningAmber,
  MdOutlineGroup,
  MdOutlineMonitorHeart,
  MdOutlineSpaceDashboard,
  MdOutlineSettings,
  MdOutlineChevronLeft,
  MdOutlineChevronRight,
} from "react-icons/md";
import { useAuth } from "../auth/AuthProvider";
import { useSidebarPin } from "../lib/useSidebarPin";
import { TenantSwitcher } from "./TenantSwitcher";

function IntegrationsIcon() {
  return <MdOutlineCable size={18} aria-hidden="true" />;
}

function ServicesIcon() {
  return <MdOutlineGridView size={18} aria-hidden="true" />;
}

function DomainsIcon() {
  return <MdOutlinePublic size={18} aria-hidden="true" />;
}

function IncidentsIcon() {
  return <MdOutlineWarningAmber size={18} aria-hidden="true" />;
}

function AdminsIcon() {
  return <MdOutlineGroup size={18} aria-hidden="true" />;
}

function PollerIcon() {
  return <MdOutlineMonitorHeart size={18} aria-hidden="true" />;
}

function OverviewIcon() {
  return <MdOutlineSpaceDashboard size={18} aria-hidden="true" />;
}

function SettingsIcon() {
  return <MdOutlineSettings size={18} aria-hidden="true" />;
}

// PinToggleIcon mirrors the handoff's collapse-direction chevron: pinned
// shows a left-pointing arrow (collapse), unpinned a right-pointing one
// (expand) - not a thumbtack, and not the same icon both ways.
function PinToggleIcon({ pinned }: { pinned: boolean }) {
  return pinned ? <MdOutlineChevronLeft size={18} aria-hidden="true" /> : <MdOutlineChevronRight size={18} aria-hidden="true" />;
}

const navItemClass = ({ isActive }: { isActive: boolean }) =>
  "flex h-9 items-center gap-2.5 rounded-md px-3 text-sm transition-colors " +
  (isActive ? "text-accent bg-[rgba(90,70,199,0.08)]" : "text-text-muted hover:bg-sidebar-hover-bg");

// Sidebar: collapsible 72px/240px shell nav (new-layout-migration, SHELL-02
// through SHELL-06). Expands on hover, stays expanded while pinned
// (useSidebarPin, T7). TenantSwitcher (T9) sits at the top; LogoutConfirmDialog
// (T8) replaces the modal this file used to inline.
export function Sidebar() {
  const { hasRole } = useAuth();
  const { pinned, togglePinned } = useSidebarPin();
  const { t } = useTranslation();
  const location = useLocation();
  const navigate = useNavigate();
  const [hovering, setHovering] = useState(false);

  const expanded = pinned || hovering;

  const domainsActive = location.pathname.startsWith("/domains") || location.pathname.startsWith("/status-pages");
  // OVW-15/16: the standalone "Visão geral" item is active on both "/"
  // (which redirects to /overview) and /overview itself.
  const overviewActive = location.pathname === "/" || location.pathname.startsWith("/overview");

  const labelClass = expanded ? "" : "sr-only";
  const groupLabelClass = "px-3 pb-1 pt-3 text-[10.5px] font-semibold uppercase tracking-wider text-text-muted";

  return (
    <aside
      data-testid="sidebar"
      onMouseEnter={() => setHovering(true)}
      onMouseLeave={() => setHovering(false)}
      className={
        "flex h-full shrink-0 flex-col overflow-hidden border-r border-divider bg-sidebar-bg px-3 py-4 transition-[width] duration-150 " +
        (expanded ? "w-[240px]" : "w-[72px]")
      }
    >
      <TenantSwitcher />

      <div className="my-3 h-px bg-divider" />

      <nav className="flex flex-col gap-0.5 overflow-y-auto">
        <NavLink
          to="/overview"
          className={
            "flex h-9 items-center gap-2.5 rounded-md px-3 text-sm transition-colors " +
            (overviewActive ? "text-accent bg-[rgba(90,70,199,0.08)]" : "text-text-muted hover:bg-sidebar-hover-bg")
          }
        >
          <OverviewIcon />
          <span className={labelClass}>{t("sidebar.overview")}</span>
        </NavLink>
        <div className={groupLabelClass}>{t("sidebar.groupMonitoring")}</div>
        <NavLink to="/services" className={navItemClass}>
          <ServicesIcon />
          <span className={labelClass}>{t("sidebar.services")}</span>
        </NavLink>
        <button
          type="button"
          onClick={() => navigate("/domains")}
          className={
            "flex h-9 cursor-pointer items-center gap-2.5 rounded-md px-3 text-left text-sm transition-colors " +
            (domainsActive ? "text-accent bg-[rgba(90,70,199,0.08)]" : "text-text-muted hover:bg-sidebar-hover-bg")
          }
        >
          <DomainsIcon />
          <span className={labelClass}>{t("sidebar.domainsStatusPages")}</span>
        </button>
        <NavLink to="/incidents" className={navItemClass}>
          <IncidentsIcon />
          <span className={labelClass}>{t("sidebar.incidents")}</span>
        </NavLink>

        <div className={groupLabelClass}>{t("sidebar.groupPlatform")}</div>
        <NavLink to="/integrations" className={navItemClass}>
          <IntegrationsIcon />
          <span className={labelClass}>{t("sidebar.integrations")}</span>
        </NavLink>
        <NavLink to="/poller-status" className={navItemClass}>
          <PollerIcon />
          <span className={labelClass}>{t("sidebar.pollerStatus")}</span>
        </NavLink>

        {hasRole(["owner"]) ? (
          <>
            <div className={groupLabelClass}>{t("sidebar.groupOrganization")}</div>
            <NavLink to="/admins" className={navItemClass}>
              <AdminsIcon />
              <span className={labelClass}>{t("sidebar.admins")}</span>
            </NavLink>
          </>
        ) : null}
      </nav>

      <div className="mt-auto flex flex-col gap-2 pt-4">
        <div className="h-px bg-divider" />

        {hasRole(["owner"]) ? (
          <NavLink to="/settings" className={navItemClass}>
            <SettingsIcon />
            <span className={labelClass}>{t("sidebar.settings")}</span>
          </NavLink>
        ) : null}

        <button
          type="button"
          onClick={togglePinned}
          aria-pressed={pinned}
          className={
            "flex cursor-pointer items-center gap-2 rounded-md px-3 py-1.5 text-left text-[12.5px] transition-colors " +
            (pinned ? "text-accent" : "text-text-muted hover:text-text")
          }
        >
          <PinToggleIcon pinned={pinned} />
          <span className={labelClass}>{t("sidebar.pinMenu")}</span>
        </button>
      </div>
    </aside>
  );
}
