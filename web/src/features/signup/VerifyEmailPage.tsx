import { useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { apiFetch } from "../../lib/apiClient";
import { AuthLayout } from "../auth/AuthLayout";

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
    <AuthLayout>
      {state === "loading" ? <p className="text-sm text-neutral-300">{t("verifyEmail.loading")}</p> : null}

      {state === "verified" ? (
        <>
          <h1 className="mb-1.5 text-[22px] font-bold tracking-tight text-text">{t("verifyEmail.successTitle")}</h1>
          <p className="text-[13.5px] leading-relaxed text-neutral-400">{t("verifyEmail.successSubtitle")}</p>
        </>
      ) : null}

      {state === "invalid" ? (
        <>
          <h1 className="mb-1.5 text-[22px] font-bold tracking-tight text-text">{t("verifyEmail.invalidTitle")}</h1>
          <p role="alert" className="text-[13.5px] leading-relaxed text-critical">
            {t("verifyEmail.invalidSubtitle")}
          </p>
        </>
      ) : null}

      {state !== "loading" ? (
        <Link to="/login" className="mt-4 inline-block text-[12.5px] text-accent hover:underline">
          {t("verifyEmail.goToLogin")}
        </Link>
      ) : null}
    </AuthLayout>
  );
}
