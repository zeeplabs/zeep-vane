import { useState, type FormEvent } from "react";
import { useNavigate, Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { MdOutlineVisibility, MdOutlineVisibilityOff } from "react-icons/md";
import { Field } from "../../components/ui/Field";
import { Button } from "../../components/ui/Button";
import { useAuth, type TwoFactorFactor } from "../../auth/AuthProvider";
import { LoginTwoFactorStep } from "./LoginTwoFactorStep";
import { ApiError } from "../../lib/apiClient";
import { AuthLayout } from "./AuthLayout";
import { OAuthButtons } from "./OAuthButtons";

function EyeIcon({ crossed }: { crossed: boolean }) {
  return crossed ? <MdOutlineVisibilityOff size={18} aria-hidden="true" /> : <MdOutlineVisibility size={18} aria-hidden="true" />;
}

export function LoginPage() {
  const { t } = useTranslation();
  const { login, verifyTwoFactor, deploymentMode } = useAuth();
  const navigate = useNavigate();

  const [step, setStep] = useState<"credentials" | "twoFactor">("credentials");
  // The challenge token lives only here, in component memory - never in the
  // URL or browser storage (LOGIN2FA-09). "Voltar" discards it.
  const [challengeToken, setChallengeToken] = useState("");
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
      const outcome = await login(email, password);
      if (outcome.kind === "twoFactorRequired") {
        // No session exists yet - switch to the verification step instead of
        // navigating (LOGIN2FA-01).
        setChallengeToken(outcome.challengeToken);
        setStep("twoFactor");
        return;
      }
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

  async function handleVerify(factor: TwoFactorFactor) {
    setError(null);
    setSubmitting(true);
    try {
      await verifyTwoFactor(challengeToken, factor);
      navigate("/");
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        setError(t("login.twoFactor.invalid"));
      } else {
        setError(t("login.twoFactor.generic"));
      }
    } finally {
      setSubmitting(false);
    }
  }

  function handleBackToCredentials() {
    setStep("credentials");
    setChallengeToken("");
    setError(null);
    setPassword("");
  }

  return (
    <AuthLayout>
      {step === "twoFactor" ? (
        <LoginTwoFactorStep
          submitting={submitting}
          error={error}
          onSubmit={handleVerify}
          onBack={handleBackToCredentials}
          onMethodChange={() => setError(null)}
        />
      ) : (
        <>
          <div className="mb-6">
            <h1 className="mb-1.5 text-[22px] font-bold tracking-tight text-text">{t("login.title")}</h1>
            <p className="text-[13.5px] leading-relaxed text-neutral-400">{t("login.subtitle")}</p>
          </div>

          <OAuthButtons />

          <div className="mb-5 flex items-center gap-2.5">
            <div className="h-px flex-1 bg-divider" />
            <span className="text-[11.5px] text-neutral-400">{t("auth.oauth.divider")}</span>
            <div className="h-px flex-1 bg-divider" />
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

          {deploymentMode === "saas" ? (
            <p className="mt-5 text-center text-[13px] text-neutral-400">
              {t("login.noAccount")} <Link to="/signup" className="font-semibold text-accent hover:underline">{t("login.createAccount")}</Link>
            </p>
          ) : null}
        </>
      )}
    </AuthLayout>
  );
}
