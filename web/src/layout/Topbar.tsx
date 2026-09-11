import { useTranslation } from "react-i18next";
import { useThemeToggle } from "../lib/useThemeToggle";
import { AvatarMenu } from "./AvatarMenu";

function SunIcon() {
  return (
    <svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <circle cx="12" cy="12" r="4" />
      <path d="M12 2v2M12 20v2M4.2 4.2l1.4 1.4M18.4 18.4l1.4 1.4M2 12h2M20 12h2M4.2 19.8l1.4-1.4M18.4 5.6l1.4-1.4" />
    </svg>
  );
}

function MoonIcon() {
  return (
    <svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8Z" />
    </svg>
  );
}

function BellIcon() {
  return (
    <svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M18 8a6 6 0 0 0-12 0c0 7-3 9-3 9h18s-3-2-3-9" />
      <path d="M13.7 21a2 2 0 0 1-3.4 0" />
    </svg>
  );
}

export interface TopbarProps {
  title: string;
}

// Topbar: 60px, page title left, theme toggle + static bell + AvatarMenu
// right (new-layout-migration, SHELL-08). The bell is deliberately inert -
// no href/onClick, no badge - since the Notifications feature doesn't
// exist yet (spec.md Assumptions: no dead route/link).
export function Topbar({ title }: TopbarProps) {
  const { t } = useTranslation();
  const { theme, toggleTheme } = useThemeToggle();

  return (
    <header className="flex h-[60px] shrink-0 items-center justify-between border-b border-divider px-7">
      <h1 className="text-[15px] font-bold text-text">{title}</h1>

      <div className="flex items-center gap-3">
        <button
          type="button"
          onClick={toggleTheme}
          aria-label={t("topbar.toggleTheme")}
          className="flex h-8 w-8 cursor-pointer items-center justify-center rounded-md text-topbar-icon hover:bg-sidebar-hover-bg"
        >
          {theme === "dark" ? <SunIcon /> : <MoonIcon />}
        </button>

        <span aria-hidden="true" className="flex h-8 w-8 items-center justify-center rounded-md text-topbar-icon">
          <BellIcon />
        </span>

        <AvatarMenu />
      </div>
    </header>
  );
}
