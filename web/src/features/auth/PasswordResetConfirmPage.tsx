import { useState, type FormEvent } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Field } from "../../components/ui/Field";
import { Button } from "../../components/ui/Button";
import { apiFetch, ApiError } from "../../lib/apiClient";
import { AuthLayout } from "./AuthLayout";

// PasswordResetConfirmPage is the landing page for the link
// PasswordResetHandler.sendPasswordResetEmail sends
// (/reset-password/:token). Unlike AcceptInvitePage, POST
// /api/auth/password-reset/confirm does not set a session cookie - it only
// changes the password - so success routes to /login instead of a hard
// reload to "/".
export function PasswordResetConfirmPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
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
    <AuthLayout>
      <div className="mb-6">
        <h1 className="mb-1.5 text-[22px] font-bold tracking-tight text-text">{t("passwordResetConfirm.title")}</h1>
        {!submitted ? (
          <p className="text-[13.5px] leading-relaxed text-neutral-400">{t("passwordResetConfirm.subtitle")}</p>
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
    </AuthLayout>
  );
}
