import { MdOutlineShield, MdOutlineBuild, MdOutlineVisibility } from "react-icons/md";
import { Tooltip } from "./Tooltip";

export type AdminRole = "owner" | "operator" | "viewer";

export interface IconRoleSelectorProps {
  role: AdminRole;
  onSelect: (role: AdminRole) => void;
}

const roles: { value: AdminRole; label: string }[] = [
  { value: "owner", label: "Owner" },
  { value: "operator", label: "Operator" },
  { value: "viewer", label: "Viewer" },
];

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
  return (
    <div role="group" aria-label="Selecionar papel" className="inline-flex gap-2">
      {roles.map((r) => {
        const Icon = icons[r.value];
        const active = r.value === role;
        return (
          <Tooltip key={r.value} label={r.label}>
            <button
              type="button"
              aria-label={r.label}
              aria-pressed={active}
              onClick={() => onSelect(r.value)}
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
