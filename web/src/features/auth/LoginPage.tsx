import { useState, type FormEvent } from "react";
import { useNavigate, Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Card } from "../../components/ui/Card";
import { Field } from "../../components/ui/Field";
import { Button } from "../../components/ui/Button";
import { useAuth } from "../../auth/AuthProvider";
import { ApiError } from "../../lib/apiClient";

function EyeIcon({ crossed }: { crossed: boolean }) {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path
        d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7-10-7-10-7z"
        stroke="currentColor"
        strokeWidth="1.5"
      />
      <circle cx="12" cy="12" r="3" stroke="currentColor" strokeWidth="1.5" />
      {crossed ? <path d="M3 3l18 18" stroke="currentColor" strokeWidth="1.5" /> : null}
    </svg>
  );
}

export function LoginPage() {
  const { t } = useTranslation();
  const { login } = useAuth();
  const navigate = useNavigate();

  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setSubmitting(true);
    try {
      await login(email, password);
      navigate("/");
    } catch (err) {
      if (err instanceof ApiError) {
        setError(err.message);
      } else {
        setError(t("login.invalidCredentials"));
      }
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="flex min-h-screen w-full items-center justify-center bg-bg px-4">
      <Card elevation="elev-md" className="w-full max-w-[380px] p-7">
        <h3 className="mb-6 text-text">{t("login.title")}</h3>
        <form onSubmit={handleSubmit} className="flex flex-col gap-4">
          <Field
            label={t("login.email")}
            type="email"
            autoComplete="username"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            required
          />
          <div className="relative">
            <Field
              label={t("login.password")}
              type={showPassword ? "text" : "password"}
              autoComplete="current-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
            />
            <button
              type="button"
              aria-label={showPassword ? "Ocultar senha" : "Mostrar senha"}
              onClick={() => setShowPassword((v) => !v)}
              className="absolute right-2 top-[30px] flex h-6 w-6 items-center justify-center text-neutral-400 hover:text-text"
            >
              <EyeIcon crossed={showPassword} />
            </button>
          </div>

          {error ? (
            <p role="alert" className="text-xs text-critical">
              {error}
            </p>
          ) : null}

          <Link to="/reset-password" className="text-[12.5px] text-accent hover:underline">
            {t("login.forgotPassword")}
          </Link>

          <Button type="submit" variant="primary" className="w-full" disabled={submitting}>
            {t("login.submit")}
          </Button>
        </form>
      </Card>
    </div>
  );
}
