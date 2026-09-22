import { useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import {
  MdOutlineShowChart,
  MdOutlineBarChart,
  MdOutlineSmartToy,
  MdOutlineMailOutline,
  MdOutlineSend,
} from "react-icons/md";
import { Button } from "../../components/ui/Button";
import { Dialog } from "../../components/ui/Dialog";
import { useAuth } from "../../auth/AuthProvider";
import { ApiError } from "../../lib/apiClient";
import { IntegrationCard, type IntegrationStatusKind } from "./IntegrationCard";
import { ConnectDatadogDrawer } from "./ConnectDatadogDrawer";
import { ConnectLLMProviderDrawer } from "./ConnectLLMProviderDrawer";
import { ConnectEmailProviderDrawer } from "./ConnectEmailProviderDrawer";
import { useIntegrationStatus } from "./hooks";
import {
  useActivateEmailProvider,
  useDisconnectEmailProvider,
  useEmailProviders,
  type EmailProviderName,
  type EmailProviderStatus,
} from "../email-providers/hooks";
import { useActivateLLMProvider, useDisconnectLLMProvider, useLLMProviders } from "../settings/hooks";

const cardHeaderBg = "var(--color-card-header-bg)";
const textMuted = "var(--color-text-muted)";

function useFormatSyncedAgo() {
  const { t } = useTranslation();
  return (iso: string | null | undefined): string => {
    if (!iso) return t("integrations.notConfigured");
    const minutes = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 60000));
    if (minutes < 1) return t("integrations.syncedNow");
    return t("integrations.syncedAgo", { count: minutes });
  };
}

interface CategorySectionProps {
  title: string;
  count: number;
  children: ReactNode;
}

function CategorySection({ title, count, children }: CategorySectionProps) {
  const { t } = useTranslation();
  return (
    <div className="mb-8">
      <div className="mb-3.5 flex items-baseline gap-2">
        <span className="text-[13px] font-bold text-text">{title}</span>
        <span className="text-xs text-text-muted">{t("integrations.count", { count })}</span>
      </div>
      <div className="grid grid-cols-1 gap-5 md:grid-cols-2 xl:grid-cols-3">{children}</div>
    </div>
  );
}

function actionButton(
  canManage: boolean,
  connected: boolean,
  onClick: () => void,
  t: (key: string) => string
): ReactNode {
  if (!canManage) return null;
  return (
    <Button type="button" variant={connected ? "secondary" : "primary"} className="w-full" onClick={onClick}>
      {connected ? t("integrations.editConnectionButton") : t("integrations.connectButton")}
    </Button>
  );
}

function DatadogCard({ canManage, onConnect }: { canManage: boolean; onConnect: () => void }) {
  const { t } = useTranslation();
  const formatSyncedAgo = useFormatSyncedAgo();
  const { data, isLoading, isError } = useIntegrationStatus();
  const connected = data?.connected ?? false;
  const status: IntegrationStatusKind = connected ? "connected" : "not_connected";

  let meta = isLoading ? t("integrations.loading") : formatSyncedAgo(connected ? data?.last_checked_at : null);
  if (isError) meta = t("integrations.loadError");

  return (
    <IntegrationCard
      icon={<MdOutlineShowChart size={24} color="var(--color-accent)" aria-hidden="true" />}
      bannerBg="#F3F1FB"
      status={status}
      title={t("integrations.datadog.title")}
      description={t("integrations.datadog.description")}
      meta={meta}
      action={isLoading || isError ? null : actionButton(canManage, connected, onConnect, t)}
    />
  );
}

function NewRelicCard() {
  const { t } = useTranslation();
  return (
    <IntegrationCard
      icon={<MdOutlineBarChart size={24} color={textMuted} aria-hidden="true" />}
      bannerBg={cardHeaderBg}
      status="coming_soon"
      title={t("integrations.newRelic.title")}
      description={t("integrations.newRelic.description")}
    />
  );
}

function LLMProviderCard({ canManage, onConnect }: { canManage: boolean; onConnect: () => void }) {
  const { t } = useTranslation();
  const { data, isLoading, isError } = useLLMProviders(1);
  const status = data?.providers.find((p) => p.provider === "openai");
  const connected = status?.status === "connected";
  const isActive = data?.active_provider === "openai";
  const kind: IntegrationStatusKind = connected ? "connected" : "not_connected";
  const activateMutation = useActivateLLMProvider();
  const disconnectMutation = useDisconnectLLMProvider();
  const [error, setError] = useState<string | null>(null);
  const [disconnectDialogOpen, setDisconnectDialogOpen] = useState(false);

  let meta = isLoading
    ? t("integrations.loading")
    : connected
      ? isActive
        ? t("integrations.llm.activeMeta", { model: status?.model })
        : t("integrations.llm.meta", { model: status?.model })
      : t("integrations.notConfigured");
  if (isError) meta = t("integrations.loadError");

  async function handleActivate() {
    setError(null);
    try {
      await activateMutation.mutateAsync("openai");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : t("aiSettings.genericActivateError"));
    }
  }

  async function handleConfirmDisconnect() {
    setError(null);
    try {
      await disconnectMutation.mutateAsync("openai");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : t("aiSettings.genericDisconnectError"));
    } finally {
      setDisconnectDialogOpen(false);
    }
  }

  return (
    <>
      <IntegrationCard
        icon={<MdOutlineSmartToy size={24} color="#B45309" aria-hidden="true" />}
        bannerBg="#FBF4E9"
        status={kind}
        title={t("integrations.llm.title")}
        description={t("integrations.llm.description")}
        meta={meta}
        action={
          isLoading || isError ? null : (
            <div className="flex flex-col gap-2">
              {error ? (
                <p role="alert" className="m-0 text-xs text-critical">
                  {error}
                </p>
              ) : null}
              {canManage && connected ? (
                <div className="flex gap-2">
                  {!isActive ? (
                    <Button
                      variant="secondary"
                      className="flex-1"
                      onClick={handleActivate}
                      disabled={activateMutation.isPending}
                    >
                      {t("aiSettings.activateButton")}
                    </Button>
                  ) : null}
                  <Button
                    variant="secondary"
                    className="flex-1 !border-critical !text-critical"
                    onClick={() => setDisconnectDialogOpen(true)}
                    disabled={disconnectMutation.isPending}
                  >
                    {t("aiSettings.disconnectButton")}
                  </Button>
                </div>
              ) : null}
              {actionButton(canManage, connected, onConnect, t)}
            </div>
          )
        }
      />
      <DisconnectConfirmDialog
        open={disconnectDialogOpen}
        onOpenChange={setDisconnectDialogOpen}
        onConfirm={handleConfirmDisconnect}
        pending={disconnectMutation.isPending}
        title={t("aiSettings.disconnectDialog.title")}
        description={t("aiSettings.disconnectDialog.body", { provider: t("aiSettings.providerLabel") })}
        cancelLabel={t("aiSettings.disconnectDialog.cancel")}
        confirmLabel={t("aiSettings.disconnectDialog.confirm")}
      />
    </>
  );
}

const EMAIL_PROVIDER_ICON: Record<EmailProviderName, { icon: ReactNode; bannerBg: string }> = {
  resend: {
    icon: <MdOutlineMailOutline size={24} color="#1A9E6B" aria-hidden="true" />,
    bannerBg: "#EEF6F1",
  },
  sendgrid: {
    icon: <MdOutlineSend size={24} color={textMuted} aria-hidden="true" />,
    bannerBg: cardHeaderBg,
  },
};

function EmailProviderCard({
  id,
  status,
  isActive,
  canManage,
  isError,
  onConnect,
}: {
  id: EmailProviderName;
  status?: EmailProviderStatus;
  isActive: boolean;
  canManage: boolean;
  isError: boolean;
  onConnect: () => void;
}) {
  const { t } = useTranslation();
  const providerIcon = EMAIL_PROVIDER_ICON[id];
  const providerTitle = t(`integrations.email.${id}.title`);
  const providerDescription = t(`integrations.email.${id}.description`);
  const connected = status?.status === "connected";
  const kind: IntegrationStatusKind = connected ? "connected" : "not_connected";
  const activateMutation = useActivateEmailProvider();
  const disconnectMutation = useDisconnectEmailProvider();
  const [error, setError] = useState<string | null>(null);
  const [disconnectDialogOpen, setDisconnectDialogOpen] = useState(false);

  async function handleActivate() {
    setError(null);
    try {
      await activateMutation.mutateAsync(id);
    } catch (err) {
      setError(
        err instanceof ApiError ? err.message : t("emailProviders.genericActivateError", { provider: providerTitle })
      );
    }
  }

  async function handleConfirmDisconnect() {
    setError(null);
    try {
      await disconnectMutation.mutateAsync(id);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : t("emailProviders.genericDisconnectError"));
    } finally {
      setDisconnectDialogOpen(false);
    }
  }

  return (
    <>
      <IntegrationCard
        icon={providerIcon.icon}
        bannerBg={providerIcon.bannerBg}
        status={kind}
        title={providerTitle}
        description={providerDescription}
        meta={
          isError
            ? t("integrations.loadError")
            : connected
              ? isActive
                ? t("integrations.email.active")
                : t("integrations.email.verified")
              : t("integrations.notConfigured")
        }
        action={
          isError ? null : (
            <div className="flex flex-col gap-2">
              {error ? (
                <p role="alert" className="m-0 text-xs text-critical">
                  {error}
                </p>
              ) : null}
              {canManage && connected ? (
                <div className="flex gap-2">
                  {!isActive ? (
                    <Button
                      variant="secondary"
                      className="flex-1"
                      onClick={handleActivate}
                      disabled={activateMutation.isPending}
                    >
                      {t("emailProviders.activateButton")}
                    </Button>
                  ) : null}
                  <Button
                    variant="secondary"
                    className="flex-1 !border-critical !text-critical"
                    onClick={() => setDisconnectDialogOpen(true)}
                    disabled={disconnectMutation.isPending}
                  >
                    {t("emailProviders.disconnectButton")}
                  </Button>
                </div>
              ) : null}
              {actionButton(canManage, connected, onConnect, t)}
            </div>
          )
        }
      />
      <DisconnectConfirmDialog
        open={disconnectDialogOpen}
        onOpenChange={setDisconnectDialogOpen}
        onConfirm={handleConfirmDisconnect}
        pending={disconnectMutation.isPending}
        title={t("emailProviders.disconnectDialog.title")}
        description={t("emailProviders.disconnectDialog.body", { provider: providerTitle })}
        cancelLabel={t("emailProviders.disconnectDialog.cancel")}
        confirmLabel={t("emailProviders.disconnectDialog.confirm")}
      />
    </>
  );
}

function DisconnectConfirmDialog({
  open,
  onOpenChange,
  onConfirm,
  pending,
  title,
  description,
  cancelLabel,
  confirmLabel,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: () => void;
  pending: boolean;
  title: string;
  description: string;
  cancelLabel: string;
  confirmLabel: string;
}) {
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={title}
      description={description}
      footer={
        <>
          <Button type="button" variant="secondary" onClick={() => onOpenChange(false)}>
            {cancelLabel}
          </Button>
          <Button
            type="button"
            variant="solid"
            className="!border-critical !bg-critical hover:!bg-critical"
            onClick={onConfirm}
            disabled={pending}
          >
            {confirmLabel}
          </Button>
        </>
      }
    />
  );
}

export function IntegrationsPage() {
  const { t } = useTranslation();
  const { hasRole } = useAuth();
  const canManage = hasRole(["owner", "operator"]);
  const { data: emailData, isError: emailIsError } = useEmailProviders(1);
  const byProvider = new Map(emailData?.providers.map((p) => [p.provider, p]));

  const [datadogDrawerOpen, setDatadogDrawerOpen] = useState(false);
  const [llmDrawerOpen, setLlmDrawerOpen] = useState(false);
  const [emailDrawerProvider, setEmailDrawerProvider] = useState<EmailProviderName | null>(null);

  return (
    <div className="mx-auto flex w-full max-w-[1280px] flex-col">
      <div className="mb-7">
        <h2 className="text-text">{t("integrations.title")}</h2>
        <p className="m-0 text-[13.5px] text-text-muted">{t("integrations.subtitle")}</p>
      </div>

      <CategorySection title={t("integrations.categories.apm")} count={2}>
        <DatadogCard canManage={canManage} onConnect={() => setDatadogDrawerOpen(true)} />
        <NewRelicCard />
      </CategorySection>

      <CategorySection title={t("integrations.categories.ai")} count={1}>
        <LLMProviderCard canManage={canManage} onConnect={() => setLlmDrawerOpen(true)} />
      </CategorySection>

      <CategorySection title={t("integrations.categories.email")} count={2}>
        <EmailProviderCard
          id="resend"
          status={byProvider.get("resend")}
          isActive={emailData?.active_provider === "resend"}
          canManage={canManage}
          isError={emailIsError}
          onConnect={() => setEmailDrawerProvider("resend")}
        />
        <EmailProviderCard
          id="sendgrid"
          status={byProvider.get("sendgrid")}
          isActive={emailData?.active_provider === "sendgrid"}
          canManage={canManage}
          isError={emailIsError}
          onConnect={() => setEmailDrawerProvider("sendgrid")}
        />
      </CategorySection>

      <ConnectDatadogDrawer open={datadogDrawerOpen} onOpenChange={setDatadogDrawerOpen} />
      <ConnectLLMProviderDrawer open={llmDrawerOpen} onOpenChange={setLlmDrawerOpen} />
      <ConnectEmailProviderDrawer
        provider={emailDrawerProvider}
        onOpenChange={(open) => {
          if (!open) setEmailDrawerProvider(null);
        }}
      />
    </div>
  );
}
