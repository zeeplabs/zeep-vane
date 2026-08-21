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
    <div role="tablist" aria-label={rest["aria-label"]} className="inline-flex h-9">
      {options.map((opt, index) => {
        const active = opt.value === value;
        return (
          <button
            key={opt.value}
            type="button"
            role="tab"
            aria-selected={active}
            onClick={() => onChange(opt.value)}
            className={
              "h-full cursor-pointer border px-4 text-sm transition-colors " +
              (index > 0 ? "-ml-px " : "") +
              (index === 0 ? "rounded-l-md " : "") +
              (index === options.length - 1 ? "rounded-r-md " : "") +
              (active
                ? "z-10 border-accent text-accent"
                : "border-divider text-neutral-300 hover:text-text")
            }
          >
            {opt.label}
          </button>
        );
      })}
    </div>
  );
}
