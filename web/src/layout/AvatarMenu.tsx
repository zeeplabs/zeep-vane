import { useEffect, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { useAuth } from "../auth/AuthProvider";
import { LogoutConfirmDialog } from "./LogoutConfirmDialog";

function initialsFor(label: string): string {
  return label.trim().slice(0, 2).toUpperCase();
}

// AvatarMenu is the topbar's user popover (new-layout-migration, SHELL-08/
// SHELL-09): identity, "My Profile" (any authenticated role, profile-page
// PROFPAGE-02), "Settings" (same owner-only gate the sidebar nav item
// already applies), and "Log out" (reuses LogoutConfirmDialog, T8 - no second
// copy of the confirm modal).
export function AvatarMenu() {
  const { t } = useTranslation();
  const { admin, hasRole, logout } = useAuth();
  const [open, setOpen] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);
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

  if (!admin) return null;

  const label = admin.name || admin.email;

  async function handleConfirmLogout() {
    setConfirmOpen(false);
    setOpen(false);
    await logout();
  }

  return (
    <div ref={containerRef} className="relative">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={t("avatarMenu.trigger")}
        className="flex h-[32px] w-[32px] cursor-pointer items-center justify-center rounded-[9px] text-[12.5px] font-bold text-accent"
        style={{ background: "color-mix(in srgb, var(--color-accent) 10%, transparent)" }}
      >
        {initialsFor(label)}
      </button>

      {open ? (
        <div
          role="menu"
          className="absolute right-0 top-full z-10 mt-1 w-[200px] rounded-md border border-divider bg-surface p-1.5 shadow-sm"
        >
          <div className="mb-1 border-b border-divider px-[10px] pb-2 pt-[10px]">
            <div className="truncate text-[13px] font-bold text-text">{label}</div>
            <div className="truncate text-xs text-text-muted">{admin.email}</div>
          </div>
          <Link
            to="/profile"
            role="menuitem"
            onClick={() => setOpen(false)}
            className="block rounded-[8px] px-[10px] py-2 text-[13px] font-semibold text-text-muted hover:bg-sidebar-hover-bg"
          >
            {t("profile.menuItem")}
          </Link>
          {hasRole(["owner"]) ? (
            <Link
              to="/settings"
              role="menuitem"
              onClick={() => setOpen(false)}
              className="block rounded-[8px] px-[10px] py-2 text-[13px] font-semibold text-text-muted hover:bg-sidebar-hover-bg"
            >
              {t("sidebar.settings")}
            </Link>
          ) : null}
          <button
            type="button"
            role="menuitem"
            onClick={() => setConfirmOpen(true)}
            className="block w-full cursor-pointer rounded-[8px] px-[10px] py-2 text-left text-[13px] font-semibold text-critical hover:bg-critical/10"
          >
            {t("sidebar.logout")}
          </button>
        </div>
      ) : null}

      <LogoutConfirmDialog open={confirmOpen} onOpenChange={setConfirmOpen} onConfirm={handleConfirmLogout} />
    </div>
  );
}
