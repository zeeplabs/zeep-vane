import { useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { MdOutlineWarningAmber } from "react-icons/md";
import { Button } from "../../components/ui/Button";
import { failureMessage } from "./format";
import { usePollerStatus } from "./hooks";

function WarningTriangleIcon() {
  return <MdOutlineWarningAmber size={18} aria-hidden="true" />;
}

export function PollerBanner() {
  // PAG-08: PollerBanner shows a summary, not a paginated list - it never
  // needs its own Pager (T18). Page 1 (page_size 20) is enough in practice
  // (AD-002: single-tenant installs have a handful of integrations at
  // most), so this deliberately doesn't scan every page for a failure.
  const { t } = useTranslation();
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
        <span className="text-sm">{failureMessage(t, failing.map((entry) => entry.provider))}</span>
      </div>
      <Button variant="ghost" onClick={() => navigate("/poller-status")}>
        {t("poller.banner.viewDetails")}
      </Button>
    </div>
  );
}
