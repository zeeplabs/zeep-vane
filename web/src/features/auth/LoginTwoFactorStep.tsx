import { useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { Field } from "../../components/ui/Field";
import { Button } from "../../components/ui/Button";
import type { TwoFactorFactor } from "../../auth/AuthProvider";

type Method = "code" | "recovery";

export interface LoginTwoFactorStepProps {
  submitting: boolean;
  error: string | null;
  onSubmit: (factor: TwoFactorFactor) => void;
  onBack: () => void;
  /** Notifies the page that the method changed so it can clear a stale inline
   * error (LOGIN2FA-07). */
  onMethodChange?: () => void;
}

export function LoginTwoFactorStep({
  submitting,
  error,
  onSubmit,
  onBack,
  onMethodChange,
}: LoginTwoFactorStepProps) {
  const { t } = useTranslation();
  const [method, setMethod] = useState<Method>("code");
  const [value, setValue] = useState("");

  function switchMethod(next: Method) {
    if (next === method) return;
    setMethod(next);
    setValue("");
    onMethodChange?.();
  }

  function handleSubmit(e: FormEvent) {
    e.preventDefault();
    onSubmit(method === "code" ? { code: value } : { recoveryCode: value });
  }

  const isCode = method === "code";

  return (
    <form onSubmit={handleSubmit} className="flex flex-col gap-4">
      <div>
        <h3 className="text-text">{t("login.twoFactor.title")}</h3>
        <p className="mt-1 text-[13.5px] text-neutral-400">{t("login.twoFactor.subtitle")}</p>
      </div>

      <Field
        label={isCode ? t("login.twoFactor.codeLabel") : t("login.twoFactor.recoveryLabel")}
        type="text"
        inputMode={isCode ? "numeric" : undefined}
        autoComplete={isCode ? "one-time-code" : "off"}
        value={value}
        onChange={(e) => setValue(e.target.value)}
        hint={isCode ? undefined : t("login.twoFactor.recoveryHint")}
        required
      />

      {error ? (
        <p role="alert" className="text-xs text-critical">
          {error}
        </p>
      ) : null}

      <Button type="submit" variant="primary" className="w-full" disabled={submitting}>
        {t("login.twoFactor.submit")}
      </Button>

      <div className="flex items-center justify-between text-[12.5px]">
        <button
          type="button"
          onClick={() => switchMethod(isCode ? "recovery" : "code")}
          className="cursor-pointer text-accent hover:underline"
        >
          {isCode ? t("login.twoFactor.useRecovery") : t("login.twoFactor.useCode")}
        </button>
        <button
          type="button"
          onClick={onBack}
          className="cursor-pointer text-neutral-400 hover:text-text"
        >
          {t("login.twoFactor.back")}
        </button>
      </div>
    </form>
  );
}
