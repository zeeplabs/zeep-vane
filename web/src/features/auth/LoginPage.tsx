import { useState, type FormEvent } from "react";
import { useNavigate, Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Field } from "../../components/ui/Field";
import { Button } from "../../components/ui/Button";
import { useAuth } from "../../auth/AuthProvider";
import { ApiError } from "../../lib/apiClient";
import { useBrandLogoUrl } from "../../lib/branding";
import vaneLogo from "../../assets/vane-logo.webp";

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
  const logoUrl = useBrandLogoUrl();

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
            <h3 className="text-text">{t("login.title")}</h3>
            <p className="mt-1 text-[13.5px] text-neutral-400">
              Entre com suas credenciais para acessar o painel.
            </p>
          </div>

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
                className="absolute right-2 top-[30px] flex h-6 w-6 cursor-pointer items-center justify-center text-neutral-400 hover:text-text"
              >
                <EyeIcon crossed={showPassword} />
              </button>
            </div>

            {error ? (
              <p role="alert" className="text-xs text-critical">
                {error}
              </p>
            ) : null}

            <Link to="/reset-password" className="-mt-1 self-end text-[12.5px] text-accent hover:underline">
              {t("login.forgotPassword")}
            </Link>

            <Button type="submit" variant="primary" className="w-full" disabled={submitting}>
              {t("login.submit")}
            </Button>
          </form>
        </div>
      </div>
    </div>
  );
}
