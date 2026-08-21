import { useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Dialog } from "../components/ui/Dialog";
import { Button } from "../components/ui/Button";
import { useAuth } from "./AuthProvider";

function LockIcon() {
  return (
    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <rect x="5" y="11" width="14" height="9" rx="2" stroke="currentColor" strokeWidth="1.5" />
      <path d="M8 11V7a4 4 0 118 0v4" stroke="currentColor" strokeWidth="1.5" />
    </svg>
  );
}

export function SessionExpiredModal() {
  const { sessionExpired, dismissSessionExpired } = useAuth();
  const navigate = useNavigate();
  const { t } = useTranslation();

  function handleGoToLogin() {
    dismissSessionExpired();
    navigate("/login");
  }

  return (
    <div className="z-[9999] relative">
      <Dialog
        open={sessionExpired}
        onOpenChange={() => {
          /* bloqueante: só fecha via CTA */
        }}
        title={t("sessionExpired.title")}
        disableBackdropDismiss
      >
        <div className="flex flex-col items-center gap-3 text-center">
          <div className="text-critical">
            <LockIcon />
          </div>
          <p className="text-sm text-neutral-300">{t("sessionExpired.body")}</p>
          <Button variant="primary" className="w-full" onClick={handleGoToLogin}>
            {t("sessionExpired.cta")}
          </Button>
        </div>
      </Dialog>
    </div>
  );
}
