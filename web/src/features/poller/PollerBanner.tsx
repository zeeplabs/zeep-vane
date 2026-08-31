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

const PROVIDER_LABELS: Record<string, string> = {
  datadog: "Datadog",
  sendgrid: "SendGrid",
  resend: "Resend",
};

function providerLabel(provider: string): string {
  return PROVIDER_LABELS[provider] ?? provider;
}

// Names the specific integration(s) so an operator doesn't have to open the
// details page just to know which credential to rotate.
function failureMessage(providers: string[]): string {
  const labels = providers.map(providerLabel);
  if (labels.length === 1) {
    return `Falha ao verificar a integração ${labels[0]} — última tentativa não teve sucesso.`;
  }
  return `Falha ao verificar as integrações ${labels.join(" e ")} — última tentativa não teve sucesso.`;
}

export function PollerBanner() {
  // PAG-08: PollerBanner shows a summary, not a paginated list - it never
  // needs its own Pager (T18). Page 1 (page_size 20) is enough in practice
  // (AD-002: single-tenant installs have a handful of integrations at
  // most), so this deliberately doesn't scan every page for a failure.
  const { data } = usePollerStatus(1);
  const navigate = useNavigate();
  const failing = (data?.items ?? []).filter((entry) => entry.status !== "active");

  if (failing.length === 0) return null;

  return (
    <div
      data-testid="poller-banner"
      className="flex items-center justify-between gap-3 px-4 py-2 text-critical"
      style={{ backgroundColor: "color-mix(in oklch, var(--color-critical) 14%, transparent)" }}
    >
      <div className="flex items-center gap-2">
        <WarningTriangleIcon />
        <span className="text-sm">{failureMessage(failing.map((entry) => entry.provider))}</span>
      </div>
      <Button variant="ghost" onClick={() => navigate("/poller-status")}>
        Ver detalhes
      </Button>
    </div>
  );
}
