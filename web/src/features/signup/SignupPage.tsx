import { useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Field } from "../../components/ui/Field";
import { Button } from "../../components/ui/Button";
import { apiFetch, ApiError } from "../../lib/apiClient";
import { useBrandLogoUrl } from "../../lib/branding";
import vaneLogo from "../../assets/vane-logo.webp";

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
  const logoUrl = useBrandLogoUrl();

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

          {pendingEmail ? (
            <>
              <div className="mb-7">
                <h3 className="text-text">{t("signup.pendingTitle")}</h3>
                <p className="mt-1 text-[13.5px] text-neutral-400">
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
              <div className="mb-7">
                <h3 className="text-text">{t("signup.title")}</h3>
                <p className="mt-1 text-[13.5px] text-neutral-400">{t("signup.subtitle")}</p>
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
        </div>
      </div>
    </div>
  );
}
