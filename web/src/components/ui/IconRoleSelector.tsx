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
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path
        d="M12 2l7 3v6c0 5-3.5 8.5-7 11-3.5-2.5-7-6-7-11V5l7-3z"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinejoin="round"
      />
    </svg>
  );
}

function WrenchIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path
        d="M14.7 6.3a4 4 0 00-5.4 5.4L3 18l3 3 6.3-6.3a4 4 0 005.4-5.4l-2.6 2.6-2-2 2.6-2.6z"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinejoin="round"
      />
    </svg>
  );
}

function EyeIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path
        d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7-10-7-10-7z"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinejoin="round"
      />
      <circle cx="12" cy="12" r="3" stroke="currentColor" strokeWidth="1.5" />
    </svg>
  );
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
                "flex h-9 w-9 cursor-pointer items-center justify-center rounded-md transition-opacity " +
                (active ? "text-accent bg-accent-900 opacity-100" : "text-text opacity-40")
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
