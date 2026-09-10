import { useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { apiFetch } from "../../lib/apiClient";
import { useBrandLogoUrl } from "../../lib/branding";
import vaneLogo from "../../assets/vane-logo.webp";

type VerifyState = "loading" | "verified" | "invalid";

// VerifyEmailPage is the destination of the link
// SignupHandler.issueAndSendVerification emails
// ("{adminBaseURL}/verify-email/{token}") - it calls GET
// /api/signup/verify/{token} once on mount and reports success/failure
// (T10, TENANT-09/10). A valid, unexpired token marks the account verified
// and login now works normally; nothing here logs the visitor in
// automatically - login is a separate, deliberate step, same as the
// backend's own contract (verifying never issues a session).
export function VerifyEmailPage() {
  const { t } = useTranslation();
  const logoUrl = useBrandLogoUrl();
  const { token } = useParams<{ token: string }>();

  const [state, setState] = useState<VerifyState>("loading");

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        // skipUnauthorizedHandler: public/unauthenticated endpoint, no
        // session exists at this point - same reasoning as every other
        // public auth call in this app.
        await apiFetch(`/api/signup/verify/${token}`, { skipUnauthorizedHandler: true });
        if (!cancelled) setState("verified");
      } catch {
        if (!cancelled) setState("invalid");
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [token]);

  return (
    <div
      className="flex min-h-screen w-full items-center justify-center bg-bg px-4"
      style={{ background: "color-mix(in srgb, var(--color-bg) 80%, black)" }}
    >
      <div className="w-full max-w-[380px] text-center">
        <img src={logoUrl ?? vaneLogo} alt="Vane" className="mx-auto mb-6 h-8 object-contain" />

        {state === "loading" ? <p className="text-sm text-neutral-300">{t("verifyEmail.loading")}</p> : null}

        {state === "verified" ? (
          <>
            <h3 className="text-text">{t("verifyEmail.successTitle")}</h3>
            <p className="mt-1 text-[13.5px] text-neutral-400">{t("verifyEmail.successSubtitle")}</p>
          </>
        ) : null}

        {state === "invalid" ? (
          <>
            <h3 className="text-text">{t("verifyEmail.invalidTitle")}</h3>
            <p role="alert" className="mt-1 text-[13.5px] text-critical">
              {t("verifyEmail.invalidSubtitle")}
            </p>
          </>
        ) : null}

        {state !== "loading" ? (
          <Link to="/login" className="mt-4 inline-block text-[12.5px] text-accent hover:underline">
            {t("verifyEmail.goToLogin")}
          </Link>
        ) : null}
      </div>
    </div>
  );
}
