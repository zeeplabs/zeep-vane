import { useTranslation } from "react-i18next";
import { MdOutlineShield, MdOutlineBuild, MdOutlineVisibility } from "react-icons/md";
import { Tooltip } from "./Tooltip";

export type AdminRole = "owner" | "operator" | "viewer";

export interface IconRoleSelectorProps {
  role: AdminRole;
  onSelect: (role: AdminRole) => void;
}

const roleValues: AdminRole[] = ["owner", "operator", "viewer"];

function ShieldIcon() {
  return <MdOutlineShield size={18} aria-hidden="true" />;
}

function WrenchIcon() {
  return <MdOutlineBuild size={18} aria-hidden="true" />;
}

function EyeIcon() {
  return <MdOutlineVisibility size={18} aria-hidden="true" />;
}

const icons: Record<AdminRole, () => JSX.Element> = {
  owner: ShieldIcon,
  operator: WrenchIcon,
  viewer: EyeIcon,
};

export function IconRoleSelector({ role, onSelect }: IconRoleSelectorProps) {
  const { t } = useTranslation();
  return (
    <div role="group" aria-label={t("components.iconRoleSelector.groupLabel")} className="inline-flex gap-2">
      {roleValues.map((value) => {
        const label = t(`components.iconRoleSelector.roleLabel.${value}`);
        const Icon = icons[value];
        const active = value === role;
        return (
          <Tooltip key={value} label={label}>
            <button
              type="button"
              aria-label={label}
              aria-pressed={active}
              onClick={() => onSelect(value)}
              className={
                "flex h-9 w-9 cursor-pointer items-center justify-center rounded-md border transition-opacity " +
                (active ? "border-accent text-accent opacity-100" : "border-divider text-text opacity-40")
              }
            >
              <Icon />
            </button>
          </Tooltip>
        );
      })}
    </div>
  );
}
