export interface SegOption {
  value: string;
  label: string;
}

export interface SegProps {
  options: SegOption[];
  value: string;
  onChange: (value: string) => void;
  "aria-label"?: string;
}

export function Seg({ options, value, onChange, ...rest }: SegProps) {
  return (
    <div
      role="tablist"
      aria-label={rest["aria-label"]}
      className="inline-flex rounded-md border border-divider bg-surface p-0.5"
    >
      {options.map((opt) => {
        const active = opt.value === value;
        return (
          <button
            key={opt.value}
            type="button"
            role="tab"
            aria-selected={active}
            onClick={() => onChange(opt.value)}
            className={
              "px-3 h-8 rounded-sm text-sm transition-colors " +
              (active
                ? "text-accent ring-1 ring-inset ring-accent"
                : "text-neutral-300 hover:text-text")
            }
          >
            {opt.label}
          </button>
        );
      })}
    </div>
  );
}
