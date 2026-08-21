import { useNavigate } from "react-router-dom";
import { Button } from "../../components/ui/Button";
import { usePollerStatus } from "./hooks";

function WarningTriangleIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path
        d="M12 3l10 18H2L12 3z"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinejoin="round"
      />
      <path d="M12 10v4" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
      <circle cx="12" cy="17" r="0.9" fill="currentColor" />
    </svg>
  );
}

export function PollerBanner() {
  const { data } = usePollerStatus();
  const navigate = useNavigate();
  const hasFailure = (data ?? []).some((entry) => entry.status !== "active");

  if (!hasFailure) return null;

  return (
    <div
      data-testid="poller-banner"
      className="flex items-center justify-between gap-3 px-4 py-2 text-critical"
      style={{ backgroundColor: "color-mix(in oklch, var(--color-critical) 14%, transparent)" }}
    >
      <div className="flex items-center gap-2">
        <WarningTriangleIcon />
        <span className="text-sm">Uma ou mais integrações estão com falha de verificação.</span>
      </div>
      <Button variant="ghost" onClick={() => navigate("/poller-status")}>
        Ver detalhes
      </Button>
    </div>
  );
}
