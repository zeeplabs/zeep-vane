import { useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Field } from "../../components/ui/Field";
import { Button } from "../../components/ui/Button";
import { apiFetch, ApiError } from "../../lib/apiClient";
import { AuthLayout } from "../auth/AuthLayout";
import { OAuthButtons } from "../auth/OAuthButtons";

interface SignupResponse {
  status: string;
  email: string;
  email_sent?: boolean;
}

// SignupPage is the public SaaS self-serve signup form (multi-tenancy-core,
// T9/TENANT-08): email + password + tenant_name, POSTed to /api/signup.
// Mirrors PasswordResetRequestPage's "form -> confirmation state" shape -
// a successful signup never logs the visitor in immediately (email
// verification is mandatory, T10/TENANT-09/10), so there's no session to
// redirect into, just a "check your email" screen with a resend action.
export function SignupPage() {
  const { t } = useTranslation();

  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [tenantName, setTenantName] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const [pendingEmail, setPendingEmail] = useState<string | null>(null);
  const [resendStatus, setResendStatus] = useState<"idle" | "sent" | "error">("idle");
  const [resending, setResending] = useState(false);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setSubmitting(true);
    try {
      // skipUnauthorizedHandler: public/unauthenticated endpoint, same
      // reasoning as LoginPage/BootstrapPage's own calls.
      const resp = await apiFetch<SignupResponse>("/api/signup", {
        method: "POST",
        body: JSON.stringify({ email, password, tenant_name: tenantName }),
        skipUnauthorizedHandler: true,
      });
      setPendingEmail(resp.email);
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        setError(t("signup.alreadyPending"));
      } else if (err instanceof ApiError && err.status === 422) {
        setError(t("signup.weakPassword"));
      } else if (err instanceof ApiError) {
        setError(err.message);
      } else {
        setError(t("signup.genericError"));
      }
    } finally {
      setSubmitting(false);
    }
  }

  async function handleResend() {
    if (!pendingEmail) return;
    setResendStatus("idle");
    setResending(true);
    try {
      await apiFetch("/api/signup/resend-verification", {
        method: "POST",
        body: JSON.stringify({ email: pendingEmail }),
        skipUnauthorizedHandler: true,
      });
      setResendStatus("sent");
    } catch {
      setResendStatus("error");
    } finally {
      setResending(false);
    }
  }

  return (
    <AuthLayout>
      {pendingEmail ? (
        <>
          <div className="mb-6">
            <h1 className="mb-1.5 text-[22px] font-bold tracking-tight text-text">{t("signup.pendingTitle")}</h1>
            <p className="text-[13.5px] leading-relaxed text-neutral-400">
              {t("signup.pendingSubtitle", { email: pendingEmail })}
            </p>
          </div>

          {resendStatus === "sent" ? (
            <p className="mb-4 text-sm text-neutral-300">{t("signup.resendConfirmation")}</p>
          ) : null}
          {resendStatus === "error" ? (
            <p role="alert" className="mb-4 text-xs text-critical">
              {t("signup.resendError")}
            </p>
          ) : null}

          <Button
            type="button"
            variant="secondary"
            className="w-full"
            disabled={resending}
            onClick={handleResend}
          >
            {t("signup.resendButton")}
          </Button>
        </>
      ) : (
        <>
          <div className="mb-6">
            <h1 className="mb-1.5 text-[22px] font-bold tracking-tight text-text">{t("signup.title")}</h1>
            <p className="text-[13.5px] leading-relaxed text-neutral-400">{t("signup.subtitle")}</p>
          </div>

          <OAuthButtons />

          <div className="mb-5 flex items-center gap-2.5">
            <div className="h-px flex-1 bg-divider" />
            <span className="text-[11.5px] text-neutral-400">{t("auth.oauth.divider")}</span>
            <div className="h-px flex-1 bg-divider" />
          </div>

          <form onSubmit={handleSubmit} className="flex flex-col gap-4">
            <Field
              label={t("signup.tenantName")}
              value={tenantName}
              onChange={(e) => setTenantName(e.target.value)}
              required
            />
            <Field
              label={t("signup.email")}
              type="email"
              autoComplete="username"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              required
            />
            <Field
              label={t("signup.password")}
              type="password"
              autoComplete="new-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
            />

            {error ? (
              <p role="alert" className="text-xs text-critical">
                {error}
              </p>
            ) : null}

            <Button type="submit" variant="primary" className="w-full" disabled={submitting}>
              {t("signup.submit")}
            </Button>
          </form>
        </>
      )}

      <Link to="/login" className="mt-4 inline-block text-[12.5px] text-accent hover:underline">
        {t("signup.backToLogin")}
      </Link>
    </AuthLayout>
  );
}
