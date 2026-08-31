import { useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Field } from "../../components/ui/Field";
import { PhoneField } from "../../components/ui/PhoneField";
import { Button } from "../../components/ui/Button";
import { apiFetch, ApiError } from "../../lib/apiClient";
import { useBrandLogoUrl } from "../../lib/branding";
import vaneLogo from "../../assets/vane-logo.webp";

// BootstrapPage lets a fresh, admin-less instance create its first owner
// from the browser instead of the manual SQL/bcrypt-script README flow it
// replaces (SHD-15 through SHD-18, SHD-20). Reuses LoginPage's desktop/
// mobile brand-block layout so the first-run screen looks like part of the
// same product, not a bolted-on setup wizard.
export function BootstrapPage() {
  const { t } = useTranslation();
  const logoUrl = useBrandLogoUrl();

  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [phone, setPhone] = useState("");
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [alreadyBootstrapped, setAlreadyBootstrapped] = useState(false);
  const [submitting, setSubmitting] = useState(false);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setAlreadyBootstrapped(false);

    if (password !== confirmPassword) {
      setError(t("bootstrap.passwordMismatch"));
      return;
    }

    setSubmitting(true);
    try {
      // skipUnauthorizedHandler: this endpoint is public/unauthenticated
      // (no session exists yet at all) - same reasoning as LoginPage's own
      // login attempt.
      await apiFetch("/api/bootstrap", {
        method: "POST",
        body: JSON.stringify({ name, email, phone, password }),
        skipUnauthorizedHandler: true,
      });
      // Hard reload, not a client-side navigate: the new owner's session
      // cookie was just set server-side, and AuthProvider's boot checks
      // (/api/auth/me, /api/bootstrap/status) only run once on mount - a
      // full reload is what re-runs them with the now-current state
      // (SHD-18, SHD-19), landing the new owner on an authenticated "/"
      // instead of bouncing back to a stale needsBootstrap guard.
      window.location.assign("/");
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        setAlreadyBootstrapped(true);
      } else if (err instanceof ApiError && err.status === 422) {
        setError(t("bootstrap.weakPassword"));
      } else if (err instanceof ApiError) {
        setError(err.message);
      } else {
        setError(t("bootstrap.genericError"));
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
            <h3 className="text-text">{t("bootstrap.title")}</h3>
            <p className="mt-1 text-[13.5px] text-neutral-400">{t("bootstrap.subtitle")}</p>
          </div>

          {alreadyBootstrapped ? (
            <div className="flex flex-col gap-3">
              <p role="alert" className="text-xs text-critical">
                {t("bootstrap.alreadyBootstrapped")}
              </p>
              <Link to="/login" className="text-[13.5px] text-accent hover:underline">
                {t("bootstrap.goToLogin")}
              </Link>
            </div>
          ) : (
            <form onSubmit={handleSubmit} className="flex flex-col gap-4">
              <Field
                label={t("bootstrap.name")}
                autoComplete="name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                required
              />
              <Field
                label={t("bootstrap.email")}
                type="email"
                autoComplete="username"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                required
              />
              <PhoneField label={t("bootstrap.phone")} onChange={setPhone} />
              <Field
                label={t("bootstrap.password")}
                type="password"
                autoComplete="new-password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                required
              />
              <Field
                label={t("bootstrap.confirmPassword")}
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
                {t("bootstrap.submit")}
              </Button>
            </form>
          )}
        </div>
      </div>
    </div>
  );
}
