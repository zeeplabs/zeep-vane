// Switch is the accessible toggle primitive the Meu Perfil Notificações
// section uses (notification-preferences). It renders a native <button> with
// role="switch", so Space/Enter activation and disabled semantics come for
// free; aria-checked carries the state to assistive tech.

export interface SwitchProps {
  checked: boolean;
  onChange: (checked: boolean) => void;
  disabled?: boolean;
  className?: string;
  "aria-label"?: string;
}

export function Switch({
  checked,
  onChange,
  disabled = false,
  className = "",
  ...rest
}: SwitchProps) {
  const track = checked ? "bg-accent" : "bg-neutral-700";
  const knob = checked ? "translate-x-4" : "translate-x-0.5";

  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      disabled={disabled}
      onClick={() => onChange(!checked)}
      className={`relative inline-flex h-5 w-9 shrink-0 cursor-pointer items-center rounded-full transition-colors disabled:opacity-45 disabled:pointer-events-none focus-visible:outline-none ${track} ${className}`.trim()}
      {...rest}
    >
      <span
        className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${knob}`}
      />
    </button>
  );
}
