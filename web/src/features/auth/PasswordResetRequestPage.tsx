import { useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Field } from "../../components/ui/Field";
import { Button } from "../../components/ui/Button";
import { apiFetch, ApiError } from "../../lib/apiClient";
import { AuthLayout } from "./AuthLayout";

export function PasswordResetRequestPage() {
  const { t } = useTranslation();

  const [email, setEmail] = useState("");
  const [submitted, setSubmitted] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setSubmitting(true);
    try {
      // skipUnauthorizedHandler: public/unauthenticated endpoint, same
      // reasoning as LoginPage/BootstrapPage's own calls. The backend
      // always responds 200 regardless of whether the email is
      // registered (account-enumeration protection), so a thrown
      // ApiError here means something actually went wrong server-side.
      await apiFetch("/api/auth/password-reset/request", {
        method: "POST",
        body: JSON.stringify({ email }),
        skipUnauthorizedHandler: true,
      });
      setSubmitted(true);
    } catch (err) {
      if (err instanceof ApiError) {
        setError(err.message);
      } else {
        setError(t("passwordReset.genericError"));
      }
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <AuthLayout>
      <div className="mb-6">
        <h1 className="mb-1.5 text-[22px] font-bold tracking-tight text-text">{t("passwordReset.title")}</h1>
        {!submitted ? (
          <p className="text-[13.5px] leading-relaxed text-neutral-400">{t("passwordReset.subtitle")}</p>
        ) : null}
      </div>

      {submitted ? (
        <p className="text-sm text-neutral-300">{t("passwordReset.confirmationMessage")}</p>
      ) : (
        <form onSubmit={handleSubmit} className="flex flex-col gap-4">
          <Field
            label={t("passwordReset.email")}
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            required
          />

          {error ? (
            <p role="alert" className="text-xs text-critical">
              {error}
            </p>
          ) : null}

          <Button type="submit" variant="primary" className="w-full" disabled={submitting}>
            {t("passwordReset.submit")}
          </Button>
        </form>
      )}
      <Link to="/login" className="mt-4 inline-block text-[12.5px] text-accent hover:underline">
        {t("passwordReset.backToLogin")}
      </Link>
    </AuthLayout>
  );
}
