import { useEffect, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { useAuth } from "../auth/AuthProvider";
import { LogoutConfirmDialog } from "./LogoutConfirmDialog";

function initialsFor(label: string): string {
  return label.trim().slice(0, 2).toUpperCase();
}

// AvatarMenu is the topbar's user popover (new-layout-migration, SHELL-08/
// SHELL-09): identity, "Configurações" (same owner-only gate the sidebar
// nav item already applies), and "Sair" (reuses LogoutConfirmDialog, T8 -
// no second copy of the confirm modal).
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
        className="flex h-8 w-8 cursor-pointer items-center justify-center rounded-md bg-accent-900 text-[12px] font-semibold text-accent"
      >
        {initialsFor(label)}
      </button>

      {open ? (
        <div
          role="menu"
          className="absolute right-0 top-full z-10 mt-1 w-56 rounded-md border border-divider bg-surface py-1 shadow-sm"
        >
          <div className="px-3 py-2">
            <div className="truncate text-[13px] font-medium text-text">{label}</div>
            <div className="truncate text-[11.5px] text-text-muted">{admin.email}</div>
          </div>
          <div className="h-px bg-divider" />
          {hasRole(["owner"]) ? (
            <Link
              to="/settings"
              role="menuitem"
              onClick={() => setOpen(false)}
              className="block px-3 py-1.5 text-[13px] text-text hover:bg-sidebar-hover-bg"
            >
              {t("sidebar.settings")}
            </Link>
          ) : null}
          <button
            type="button"
            role="menuitem"
            onClick={() => setConfirmOpen(true)}
            className="block w-full cursor-pointer px-3 py-1.5 text-left text-[13px] text-text hover:bg-sidebar-hover-bg"
          >
            {t("sidebar.logout")}
          </button>
        </div>
      ) : null}

      <LogoutConfirmDialog open={confirmOpen} onOpenChange={setConfirmOpen} onConfirm={handleConfirmLogout} />
    </div>
  );
}
