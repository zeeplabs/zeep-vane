import { useEffect, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { Drawer, drawerFooterPrimaryStyle, drawerFooterSecondaryStyle } from "../../components/ui/Drawer";
import { Button } from "../../components/ui/Button";
import { Field } from "../../components/ui/Field";
import { ApiError } from "../../lib/apiClient";
import { useConnectEmailProvider, type EmailProviderName } from "../email-providers/hooks";

export interface ConnectEmailProviderDrawerProps {
  provider: EmailProviderName | null;
  onOpenChange: (open: boolean) => void;
}

/** Painel "Conectar {Resend|SendGrid}" - mesmo chrome padrão de Drawer
 * usado em "Anexar domínio"/"Criar status page" (INTG-10). `provider` nulo
 * fecha o painel, igual ao padrão `pageId`/`EditStatusPageDrawer`. */
export function ConnectEmailProviderDrawer({ provider, onOpenChange }: ConnectEmailProviderDrawerProps) {
  const { t } = useTranslation();
  const connectMutation = useConnectEmailProvider(provider ?? "resend");
  const [apiKey, setApiKey] = useState("");
  const [fromEmail, setFromEmail] = useState("");
  const [fromName, setFromName] = useState("");
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (provider) {
      setApiKey("");
      setFromEmail("");
      setFromName("");
      setError(null);
    }
  }, [provider]);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    if (!provider) return;
    setError(null);
    try {
      await connectMutation.mutateAsync({ api_key: apiKey, from_email: fromEmail, from_name: fromName });
      onOpenChange(false);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : t("integrations.email.connectError"));
    }
  }

  const label = provider ? t(`integrations.email.${provider}.title`) : "";

  return (
    <Drawer
      open={provider !== null}
      onOpenChange={onOpenChange}
      title={t("integrations.email.drawerTitle", { provider: label })}
      description={t("aiSettings.keyHint")}
      closeLabel={t("common.close")}
      footer={
        <>
          <Button
            type="button"
            variant="secondary"
            style={drawerFooterSecondaryStyle}
            onClick={() => onOpenChange(false)}
          >
            {t("common.cancel")}
          </Button>
          <Button
            type="submit"
            form="connect-email-provider-form"
            variant="solid"
            style={drawerFooterPrimaryStyle}
            disabled={connectMutation.isPending}
          >
            {t("integrations.saveButton")}
          </Button>
        </>
      }
    >
      <form id="connect-email-provider-form" onSubmit={handleSubmit} className="flex flex-col gap-3">
        <Field
          variant="filled"
          label={t("integrations.email.apiKeyLabel")}
          type="password"
          placeholder={t("integrations.email.apiKeyPlaceholder")}
          value={apiKey}
          onChange={(e) => setApiKey(e.target.value)}
          required
        />
        <Field
          variant="filled"
          label={t("integrations.email.fromEmailLabel")}
          type="email"
          placeholder={t("integrations.email.fromEmailPlaceholder")}
          value={fromEmail}
          onChange={(e) => setFromEmail(e.target.value)}
          required
        />
        <Field
          variant="filled"
          label={t("integrations.email.fromNameLabel")}
          placeholder={t("integrations.email.fromNamePlaceholder")}
          value={fromName}
          onChange={(e) => setFromName(e.target.value)}
          required
        />
        {error ? (
          <p role="alert" className="text-xs text-critical">
            {error}
          </p>
        ) : null}
      </form>
    </Drawer>
  );
}
