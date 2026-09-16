import { useTranslation } from "react-i18next";
import { MdOutlineWbSunny, MdOutlineNightlight, MdOutlineNotificationsNone } from "react-icons/md";
import { useThemeToggle } from "../lib/useThemeToggle";
import { AvatarMenu } from "./AvatarMenu";

function SunIcon() {
  return <MdOutlineWbSunny size={19} aria-hidden="true" />;
}

function MoonIcon() {
  return <MdOutlineNightlight size={19} aria-hidden="true" />;
}

function BellIcon() {
  return <MdOutlineNotificationsNone size={19} aria-hidden="true" />;
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
      {/* Not an <h1> - the page content's own h1 (e.g. OverviewPage's
          "Visão geral") is the page's single heading, matching the
          handoff's topbar title (a plain styled div, not a heading). Two
          <h1>s with the same text was also an a11y bug (ambiguous
          getByRole("heading", level: 1)). */}
      <p className="m-0 text-[15px] font-bold text-text">{title}</p>

      <div className="flex items-center gap-4">
        <button
          type="button"
          onClick={toggleTheme}
          aria-label={t("topbar.toggleTheme")}
          className="flex cursor-pointer items-center text-topbar-icon"
        >
          {theme === "dark" ? <SunIcon /> : <MoonIcon />}
        </button>

        <span aria-hidden="true" className="flex items-center text-topbar-icon">
          <BellIcon />
        </span>

        <AvatarMenu />
      </div>
    </header>
  );
}
