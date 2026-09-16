import { useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { MdOutlineLock } from "react-icons/md";
import { Dialog } from "../components/ui/Dialog";
import { Button } from "../components/ui/Button";
import { useAuth } from "./AuthProvider";

function LockIcon() {
  return <MdOutlineLock size={20} aria-hidden="true" />;
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
