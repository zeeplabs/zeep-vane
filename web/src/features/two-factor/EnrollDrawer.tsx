// EnrollDrawer runs the TOTP enrollment flow in three steps
// (profile-page PROFPAGE-13..16): scan (QR + manual secret) -> verify
// (6-digit code) -> codes (the 10 one-time recovery codes, shown exactly
// once). The backend returns the recovery codes only in the confirm
// response, so the dedicated step is the user's only chance to save them -
// the warning makes that explicit before finishing.
//
// onEnabled is called when the flow completes (or when the backend reports
// 409 already-enabled on open), letting the host card refresh the
// authenticated identity so it flips to the enabled state.

import { useEffect, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { QRCodeSVG } from "qrcode.react";
import { ApiError } from "../../lib/apiClient";
import { Button } from "../../components/ui/Button";
import { Drawer } from "../../components/ui/Drawer";
import { Field } from "../../components/ui/Field";
import { useConfirm2FA, useEnroll2FA } from "./hooks";

export interface EnrollDrawerProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onEnabled: () => void;
}

type Step = "scan" | "verify" | "codes";

export function EnrollDrawer({ open, onOpenChange, onEnabled }: EnrollDrawerProps) {
  const { t } = useTranslation();
  const enroll = useEnroll2FA();
  const confirm = useConfirm2FA();
  const [step, setStep] = useState<Step>("scan");
  const [code, setCode] = useState("");
  const [codeError, setCodeError] = useState<string | null>(null);
  const [recoveryCodes, setRecoveryCodes] = useState<string[]>([]);

  // Start a fresh enrollment every time the drawer opens, resetting any
  // state from a previous attempt.
  useEffect(() => {
    if (!open) return;
    setStep("scan");
    setCode("");
    setCodeError(null);
    setRecoveryCodes([]);
    enroll.mutate(undefined, {
      onError: (err) => {
        if (err instanceof ApiError && err.status === 409) {
          // Already enabled: not an error state - refresh and inform.
          toast.info(t("twoFactor.scan.alreadyEnabled"));
          onOpenChange(false);
          onEnabled();
        } else {
          toast.error(t("twoFactor.scan.loadError"));
        }
      },
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const handleVerify = (event: FormEvent) => {
    event.preventDefault();
    confirm.mutate(code, {
      onSuccess: (response) => {
        setRecoveryCodes(response.recovery_codes);
        setCodeError(null);
        setStep("codes");
      },
      onError: () => setCodeError(t("twoFactor.verify.invalid")),
    });
  };

  const handleFinish = () => {
    onEnabled();
    onOpenChange(false);
  };

  const recoveryCodesText = recoveryCodes.join("\n");

  const handleCopy = async () => {
    await navigator.clipboard.writeText(recoveryCodesText);
    toast.success(t("twoFactor.codes.copied"));
  };

  const handleDownload = () => {
    const blob = new Blob([recoveryCodesText], { type: "text/plain" });
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = "vane-recovery-codes.txt";
    link.click();
    URL.revokeObjectURL(url);
  };

  const footer =
    step === "scan" ? (
      <Button onClick={() => setStep("verify")} disabled={!enroll.data}>
        {t("twoFactor.scan.continueButton")}
      </Button>
    ) : step === "verify" ? (
      <>
        <Button variant="secondary" onClick={() => setStep("scan")}>
          {t("twoFactor.verify.back")}
        </Button>
        <Button onClick={handleVerify} disabled={confirm.isPending}>
          {t("twoFactor.verify.submit")}
        </Button>
      </>
    ) : (
      <Button onClick={handleFinish}>{t("twoFactor.codes.finish")}</Button>
    );

  return (
    <Drawer
      open={open}
      onOpenChange={onOpenChange}
      title={t("twoFactor.title")}
      description={t("twoFactor.subtitle")}
      footer={footer}
    >
      {step === "scan" ? (
        <div className="flex flex-col items-center gap-4 text-center">
          <p className="m-0 text-sm text-neutral-400">{t("twoFactor.scan.instructions")}</p>
          {enroll.data ? (
            <div data-testid="enroll-qr" className="rounded-md bg-white p-4">
              <QRCodeSVG value={enroll.data.otpauth_uri} size={176} />
            </div>
          ) : null}
          <div className="w-full">
            <p className="m-0 mb-1 text-xs text-neutral-400">{t("twoFactor.scan.manualHint")}</p>
            <code data-testid="enroll-secret" className="block break-all rounded-sm bg-neutral-900 px-3 py-2 text-sm text-text">
              {enroll.data?.secret ?? ""}
            </code>
          </div>
        </div>
      ) : step === "verify" ? (
        <form onSubmit={handleVerify} className="flex flex-col gap-4">
          <p className="m-0 text-sm text-neutral-400">{t("twoFactor.verify.instructions")}</p>
          <Field
            label={t("twoFactor.verify.title")}
            placeholder={t("twoFactor.verify.placeholder")}
            value={code}
            onChange={(e) => setCode(e.target.value)}
            error={codeError ?? undefined}
            inputMode="numeric"
            autoComplete="one-time-code"
          />
        </form>
      ) : (
        <div className="flex flex-col gap-4">
          <h3 className="m-0 text-base font-medium text-text">{t("twoFactor.codes.title")}</h3>
          <p className="m-0 text-sm text-neutral-400">{t("twoFactor.codes.instructions")}</p>
          <p className="m-0 text-sm font-medium text-warning">{t("twoFactor.codes.warning")}</p>
          <ul data-testid="recovery-codes" className="m-0 grid grid-cols-2 gap-2 p-0">
            {recoveryCodes.map((recoveryCode) => (
              <li
                key={recoveryCode}
                data-testid="recovery-code"
                className="list-none rounded-sm bg-neutral-900 px-3 py-1.5 text-center font-mono text-sm text-text"
              >
                {recoveryCode}
              </li>
            ))}
          </ul>
          <div className="flex gap-2">
            <Button variant="secondary" onClick={handleCopy}>
              {t("twoFactor.codes.copy")}
            </Button>
            <Button variant="secondary" onClick={handleDownload}>
              {t("twoFactor.codes.download")}
            </Button>
          </div>
        </div>
      )}
    </Drawer>
  );
}
