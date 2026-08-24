import { useState } from "react";
import { NavLink, useLocation, useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { useAuth } from "../auth/AuthProvider";
import { Dialog } from "../components/ui/Dialog";
import { Button } from "../components/ui/Button";
import { useBrandLogoUrl } from "../lib/branding";
import type { Role } from "../types/api";

function BrandIcon() {
  return (
    <svg
      width="20"
      height="20"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.6"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M12 2v6M12 16v6M4.9 4.9l4.2 4.2M14.9 14.9l4.2 4.2M2 12h6M16 12h6M4.9 19.1l4.2-4.2M14.9 9.1l4.2-4.2" />
    </svg>
  );
}

function IntegrationsIcon() {
  return (
    <svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M9 2v4M15 2v4M7 8h2v4a3 3 0 0 0 3 3 3 3 0 0 0 3-3V8h2M12 15v4M9 22h6" />
    </svg>
  );
}

function DomainsIcon() {
  return (
    <svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <circle cx="12" cy="12" r="9" />
      <path d="M3 12h18M12 3c2.5 2.7 4 6.1 4 9s-1.5 6.3-4 9c-2.5-2.7-4-6.1-4-9s1.5-6.3 4-9Z" />
    </svg>
  );
}

function IncidentsIcon() {
  return (
    <svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M12 3 2 20h20L12 3Z" />
      <path d="M12 10v4M12 17h.01" />
    </svg>
  );
}

function AdminsIcon() {
  return (
    <svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <circle cx="9" cy="8" r="3.2" />
      <path d="M2.8 19c.7-3.4 3.2-5.5 6.2-5.5s5.5 2.1 6.2 5.5" />
      <circle cx="17.5" cy="8.5" r="2.4" />
      <path d="M16 13.8c2.2.4 3.9 2.1 4.4 4.4" />
    </svg>
  );
}

function PollerIcon() {
  return (
    <svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M2 12h4l2 7 4-14 2 7h8" />
    </svg>
  );
}

function SettingsIcon() {
  return (
    <svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <circle cx="12" cy="12" r="3" />
      <path d="M19.4 15a1.7 1.7 0 0 0 .34 1.87l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.7 1.7 0 0 0-1.87-.34 1.7 1.7 0 0 0-1.04 1.56V21a2 2 0 1 1-4 0v-.09A1.7 1.7 0 0 0 9 19.4a1.7 1.7 0 0 0-1.87.34l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06A1.7 1.7 0 0 0 4.6 15a1.7 1.7 0 0 0-1.56-1.04H3a2 2 0 1 1 0-4h.09A1.7 1.7 0 0 0 4.6 9a1.7 1.7 0 0 0-.34-1.87l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06A1.7 1.7 0 0 0 9 4.6a1.7 1.7 0 0 0 1.04-1.56V3a2 2 0 1 1 4 0v.09A1.7 1.7 0 0 0 15 4.6a1.7 1.7 0 0 0 1.87-.34l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06A1.7 1.7 0 0 0 19.4 9a1.7 1.7 0 0 0 1.56 1.04H21a2 2 0 1 1 0 4h-.09A1.7 1.7 0 0 0 19.4 15Z" />
    </svg>
  );
}

function SimulateExpiredIcon() {
  return (
    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <rect x="4" y="10" width="16" height="10" rx="2" />
      <path d="M8 10V7a4 4 0 0 1 8 0v3" />
    </svg>
  );
}

function LogoutIcon() {
  return (
    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4M16 17l5-5-5-5M21 12H9" />
    </svg>
  );
}

const navItemClass = ({ isActive }: { isActive: boolean }) =>
  "flex h-9 items-center gap-2.5 rounded-md px-3 text-sm transition-colors " +
  (isActive ? "text-accent bg-accent-900" : "text-neutral-300 hover:text-text");

const DEV_ROLES: { value: Role; label: string }[] = [
  { value: "owner", label: "Owner" },
  { value: "operator", label: "Operator" },
  { value: "viewer", label: "Viewer" },
];

export function Sidebar() {
  const { admin, hasRole, logout, setDevRole, simulateSessionExpired } = useAuth();
  const logoUrl = useBrandLogoUrl();
  const { t } = useTranslation();
  const location = useLocation();
  const navigate = useNavigate();
  const [confirmOpen, setConfirmOpen] = useState(false);

  async function handleConfirmLogout() {
    setConfirmOpen(false);
    await logout();
  }

  const domainsActive = location.pathname.startsWith("/domains") || location.pathname.startsWith("/status-pages");

  return (
    <aside className="flex h-full w-[236px] shrink-0 flex-col border-r border-divider bg-bg px-3 py-4">
      <div className="flex items-center gap-2 px-2 pb-4 text-accent">
        {logoUrl ? (
          <img src={logoUrl} alt={t("sidebar.brand")} className="h-5 w-5 flex-none object-contain" />
        ) : (
          <BrandIcon />
        )}
        <span className="text-h5 font-medium text-text">{t("sidebar.brand")}</span>
      </div>

      <nav className="flex flex-col gap-0.5">
        <NavLink to="/integrations" className={navItemClass}>
          <IntegrationsIcon />
          <span>{t("sidebar.integrations")}</span>
        </NavLink>
        <button
          type="button"
          onClick={() => navigate("/domains")}
          className={
            "flex h-9 cursor-pointer items-center gap-2.5 rounded-md px-3 text-left text-sm transition-colors " +
            (domainsActive ? "text-accent bg-accent-900" : "text-neutral-300 hover:text-text")
          }
        >
          <DomainsIcon />
          <span>{t("sidebar.domainsStatusPages")}</span>
        </button>
        <NavLink to="/incidents" className={navItemClass}>
          <IncidentsIcon />
          <span>{t("sidebar.incidents")}</span>
        </NavLink>
        {hasRole(["owner"]) ? (
          <NavLink to="/admins" className={navItemClass}>
            <AdminsIcon />
            <span>{t("sidebar.admins")}</span>
          </NavLink>
        ) : null}
        <NavLink to="/poller-status" className={navItemClass}>
          <PollerIcon />
          <span>{t("sidebar.pollerStatus")}</span>
        </NavLink>
        {hasRole(["owner"]) ? (
          <NavLink to="/settings" className={navItemClass}>
            <SettingsIcon />
            <span>{t("sidebar.settings")}</span>
          </NavLink>
        ) : null}
      </nav>

      <div className="mt-auto flex flex-col gap-2 pt-4">
        <div className="h-px bg-divider" />

        {import.meta.env.DEV ? (
          <div className="px-2 py-0.5">
            <div className="mb-1.5 text-[10px] uppercase tracking-wider text-neutral-400 opacity-70">
              {t("sidebar.viewingAs")}
            </div>
            <div className="flex w-full rounded-md border border-divider bg-bg p-0.5" role="radiogroup" aria-label={t("sidebar.viewingAs")}>
              {DEV_ROLES.map((r) => {
                const active = admin?.role === r.value;
                return (
                  <button
                    key={r.value}
                    type="button"
                    role="radio"
                    aria-checked={active}
                    onClick={() => setDevRole(r.value)}
                    className={
                      "flex-1 cursor-pointer rounded-sm px-1 py-1.5 text-[10.5px] transition-colors " +
                      (active ? "text-accent ring-1 ring-inset ring-accent" : "text-neutral-400 hover:text-text")
                    }
                  >
                    {r.label}
                  </button>
                );
              })}
            </div>
          </div>
        ) : null}

        <button
          type="button"
          onClick={simulateSessionExpired}
          className="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-left text-[12.5px] text-text opacity-55 transition-opacity hover:opacity-80"
        >
          <SimulateExpiredIcon />
          {t("sidebar.simulateSessionExpired")}
        </button>
        <button
          type="button"
          onClick={() => setConfirmOpen(true)}
          className="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-left text-[12.5px] text-text opacity-55 transition-opacity hover:opacity-80"
        >
          <LogoutIcon />
          {t("sidebar.logout")}
        </button>

        <Dialog
          open={confirmOpen}
          onOpenChange={setConfirmOpen}
          title={t("logoutDialog.title")}
          description={t("logoutDialog.body")}
        >
          <div className="flex justify-end gap-2">
            <Button variant="secondary" onClick={() => setConfirmOpen(false)}>
              {t("logoutDialog.cancel")}
            </Button>
            <Button variant="primary" onClick={handleConfirmLogout}>
              {t("logoutDialog.confirm")}
            </Button>
          </div>
        </Dialog>
      </div>
    </aside>
  );
}
