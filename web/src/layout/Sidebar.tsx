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

// navJustify mirrors the handoff: nav items center their icon when the rail
// is collapsed (72px) instead of staying left-padded - see
// handoff-new-layout/Visao Geral.dc.html's `navJustify` prop.
const makeNavItemClass =
  (expanded: boolean) =>
  ({ isActive }: { isActive: boolean }) =>
    "flex h-9 items-center rounded-md px-3 text-sm transition-[background-color,color,gap] duration-200 ease-out " +
    (expanded ? "justify-start gap-2.5" : "justify-center gap-0") + " " +
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

  // Labels fade+grow in sync with the aside's own width transition instead
  // of an instant sr-only swap - the previous binary toggle popped text in
  // mid-transition, out of step with the width animation, which read as
  // janky. overflow-hidden clips the growing max-width so text never
  // wraps/overflows while it's still animating in.
  const labelClass =
    "overflow-hidden whitespace-nowrap transition-[opacity,max-width] duration-200 ease-out " +
    (expanded ? "max-w-[170px] opacity-100" : "max-w-0 opacity-0");
  const groupLabelClass =
    "overflow-hidden whitespace-nowrap px-3 text-[10.5px] font-semibold uppercase tracking-wider text-text-muted " +
    "transition-[opacity,max-height,padding] duration-200 ease-out " +
    (expanded ? "max-h-[28px] pb-1 pt-3 opacity-100" : "max-h-0 py-0 opacity-0");
  const navItemClass = makeNavItemClass(expanded);
  const justifyClass = expanded ? "justify-start gap-2.5" : "justify-center gap-0";

  return (
    <aside
      data-testid="sidebar"
      onMouseEnter={() => setHovering(true)}
      onMouseLeave={() => setHovering(false)}
      className={
        "flex h-full shrink-0 flex-col overflow-hidden border-r border-divider bg-sidebar-bg px-3 py-4 transition-[width] duration-200 ease-out " +
        (expanded ? "w-[240px]" : "w-[72px]")
      }
    >
      <TenantSwitcher expanded={expanded} />

      <div className="my-3 h-px bg-divider" />

      <nav className="flex flex-col gap-0.5 overflow-y-auto">
        <NavLink
          to="/overview"
          className={
            "flex h-9 items-center rounded-md px-3 text-sm transition-[background-color,color,gap] duration-200 ease-out " +
            justifyClass + " " +
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
            "flex h-9 cursor-pointer items-center rounded-md px-3 text-left text-sm transition-[background-color,color,gap] duration-200 ease-out " +
            justifyClass + " " +
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
            "flex cursor-pointer items-center rounded-md px-3 py-1.5 text-left text-[12.5px] transition-[background-color,color,gap] duration-200 ease-out " +
            justifyClass + " " +
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
