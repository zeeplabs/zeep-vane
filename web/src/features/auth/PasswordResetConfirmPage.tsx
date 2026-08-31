import { useState, type FormEvent } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Field } from "../../components/ui/Field";
import { Button } from "../../components/ui/Button";
import { apiFetch, ApiError } from "../../lib/apiClient";
import { useBrandLogoUrl } from "../../lib/branding";
import vaneLogo from "../../assets/vane-logo.webp";

// PasswordResetConfirmPage is the landing page for the link
// PasswordResetHandler.sendPasswordResetEmail sends
// (/reset-password/:token). Unlike AcceptInvitePage, POST
// /api/auth/password-reset/confirm does not set a session cookie - it only
// changes the password - so success routes to /login instead of a hard
// reload to "/".
export function PasswordResetConfirmPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const logoUrl = useBrandLogoUrl();
  const { token } = useParams<{ token: string }>();

  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [submitted, setSubmitted] = useState(false);
  const [submitting, setSubmitting] = useState(false);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);

    if (password !== confirmPassword) {
      setError(t("passwordResetConfirm.passwordMismatch"));
      return;
    }

    setSubmitting(true);
    try {
      // skipUnauthorizedHandler: public/unauthenticated endpoint - no
      // session exists yet, same reasoning as AcceptInvitePage's own call.
      await apiFetch("/api/auth/password-reset/confirm", {
        method: "POST",
        body: JSON.stringify({ token, new_password: password }),
        skipUnauthorizedHandler: true,
      });
      setSubmitted(true);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        setError(t("passwordResetConfirm.invalidOrExpired"));
      } else if (err instanceof ApiError && err.status === 422) {
        setError(t("passwordResetConfirm.weakPassword"));
      } else if (err instanceof ApiError) {
        setError(err.message);
      } else {
        setError(t("passwordResetConfirm.genericError"));
      }
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="grid min-h-screen w-full grid-cols-1 bg-bg lg:grid-cols-[minmax(0,1.1fr)_minmax(0,1fr)]">
      <div className="relative hidden overflow-hidden border-r border-divider lg:flex lg:flex-col lg:justify-between lg:p-12">
        <div
          aria-hidden="true"
          className="pointer-events-none absolute -left-40 -top-40 h-[520px] w-[520px] rounded-full opacity-40 blur-3xl"
          style={{
            background:
              "radial-gradient(circle, var(--color-accent) 0%, var(--color-accent-2) 45%, transparent 70%)",
          }}
        />
        <div
          aria-hidden="true"
          className="pointer-events-none absolute -bottom-56 -right-24 h-[420px] w-[420px] rounded-full opacity-25 blur-3xl"
          style={{ background: "radial-gradient(circle, var(--color-accent-2) 0%, transparent 70%)" }}
        />

        <div className="relative flex items-center gap-2">
          <img src={logoUrl ?? vaneLogo} alt="Company logo" className="w-[180px] object-contain" />
        </div>

        <div className="relative flex flex-col gap-4">
          <h1 className="max-w-md text-[32px] font-medium leading-tight text-text">
            Status e incidentes, sob controle.
          </h1>
          <p className="max-w-sm text-[14.5px] leading-relaxed text-neutral-300">
            Monitore integrações, comunique incidentes e mantenha suas status pages sempre
            atualizadas — tudo em um painel só.
          </p>
        </div>

        <p className="relative text-xs text-neutral-500">© {new Date().getFullYear()} Vane. Todos os direitos reservados.</p>
      </div>

      <div
        className="flex w-full items-center justify-center px-4 py-12"
        style={{ background: "color-mix(in srgb, var(--color-bg) 80%, black)" }}
      >
        <div className="w-full max-w-[380px]">
          <div className="mb-8 flex flex-col gap-1 lg:hidden">
            <div className="flex items-center gap-2">
              {logoUrl ? (
                <>
                  <img src={logoUrl} alt="" className="h-5 w-5 object-contain" />
                  <span className="text-[15px] font-medium tracking-tight text-text">Vane</span>
                </>
              ) : (
                <img src={vaneLogo} alt="Vane" className="h-6 object-contain" />
              )}
            </div>
          </div>

          <div className="mb-7">
            <h3 className="text-text">{t("passwordResetConfirm.title")}</h3>
            {!submitted ? (
              <p className="mt-1 text-[13.5px] text-neutral-400">{t("passwordResetConfirm.subtitle")}</p>
            ) : null}
          </div>

          {submitted ? (
            <>
              <p className="text-sm text-neutral-300">{t("passwordResetConfirm.successMessage")}</p>
              <Button
                type="button"
                variant="primary"
                className="mt-4 w-full"
                onClick={() => navigate("/login")}
              >
                {t("passwordResetConfirm.goToLogin")}
              </Button>
            </>
          ) : (
            <form onSubmit={handleSubmit} className="flex flex-col gap-4">
              <Field
                label={t("passwordResetConfirm.password")}
                type="password"
                autoComplete="new-password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                required
              />
              <Field
                label={t("passwordResetConfirm.confirmPassword")}
                type="password"
                autoComplete="new-password"
                value={confirmPassword}
                onChange={(e) => setConfirmPassword(e.target.value)}
                required
              />

              {error ? (
                <p role="alert" className="text-xs text-critical">
                  {error}
                </p>
              ) : null}

              <Button type="submit" variant="primary" className="w-full" disabled={submitting}>
                {t("passwordResetConfirm.submit")}
              </Button>
            </form>
          )}
          {!submitted ? (
            <Link to="/login" className="mt-4 inline-block text-[12.5px] text-accent hover:underline">
              {t("passwordReset.backToLogin")}
            </Link>
          ) : null}
        </div>
      </div>
    </div>
  );
}
