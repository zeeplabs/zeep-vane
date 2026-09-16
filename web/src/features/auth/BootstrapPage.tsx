import { useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Field } from "../../components/ui/Field";
import { PhoneField } from "../../components/ui/PhoneField";
import { Button } from "../../components/ui/Button";
import { apiFetch, ApiError } from "../../lib/apiClient";
import { AuthLayout } from "./AuthLayout";

// BootstrapPage lets a fresh, admin-less instance create its first owner
// from the browser instead of the manual SQL/bcrypt-script README flow it
// replaces (SHD-15 through SHD-18, SHD-20). Reuses LoginPage's desktop/
// mobile brand-block layout so the first-run screen looks like part of the
// same product, not a bolted-on setup wizard.
export function BootstrapPage() {
  const { t } = useTranslation();

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
    <AuthLayout>
      <div className="mb-2 text-[11px] font-bold uppercase tracking-[0.08em] text-accent">
        {t("bootstrap.eyebrow")}
      </div>
      <div className="mb-6">
        <h1 className="mb-1.5 text-[22px] font-bold tracking-tight text-text">{t("bootstrap.title")}</h1>
        <p className="text-[13.5px] leading-relaxed text-neutral-400">{t("bootstrap.subtitle")}</p>
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
    </AuthLayout>
  );
}
