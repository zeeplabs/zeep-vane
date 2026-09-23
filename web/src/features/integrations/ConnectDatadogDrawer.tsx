import { useEffect, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { Drawer, drawerFooterPrimaryStyle, drawerFooterSecondaryStyle } from "../../components/ui/Drawer";
import { Button } from "../../components/ui/Button";
import { Field } from "../../components/ui/Field";
import { ApiError } from "../../lib/apiClient";
import { useConnectDatadog } from "./hooks";

export interface ConnectDatadogDrawerProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

/** Painel "Conectar Datadog" - mesmo chrome padrão de Drawer usado em
 * "Anexar domínio"/"Criar status page" (INTG-08). */
export function ConnectDatadogDrawer({ open, onOpenChange }: ConnectDatadogDrawerProps) {
  const { t } = useTranslation();
  const connectMutation = useConnectDatadog();
  const [apiKey, setApiKey] = useState("");
  const [appKey, setAppKey] = useState("");
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (open) {
      setApiKey("");
      setAppKey("");
      setError(null);
    }
  }, [open]);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    try {
      await connectMutation.mutateAsync({ api_key: apiKey, app_key: appKey });
      onOpenChange(false);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : t("integrations.datadog.connectError"));
    }
  }

  return (
    <Drawer
      open={open}
      onOpenChange={onOpenChange}
      title={t("integrations.datadog.drawerTitle")}
      description={t("integrations.datadog.drawerDescription")}
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
            form="connect-datadog-form"
            variant="solid"
            style={drawerFooterPrimaryStyle}
            disabled={connectMutation.isPending}
          >
            {t("integrations.saveButton")}
          </Button>
        </>
      }
    >
      <form id="connect-datadog-form" onSubmit={handleSubmit} className="flex flex-col gap-3">
        <Field
          variant="filled"
          label={t("integrations.datadog.apiKeyLabel")}
          type="password"
          placeholder={t("integrations.datadog.apiKeyPlaceholder")}
          value={apiKey}
          onChange={(e) => setApiKey(e.target.value)}
          required
        />
        <Field
          variant="filled"
          label={t("integrations.datadog.appKeyLabel")}
          type="password"
          placeholder={t("integrations.datadog.appKeyPlaceholder")}
          value={appKey}
          onChange={(e) => setAppKey(e.target.value)}
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
