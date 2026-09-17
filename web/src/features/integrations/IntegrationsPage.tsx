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
import { useLLMProviders } from "../settings/hooks";

const cardHeaderBg = "var(--color-card-header-bg)";
const textMuted = "var(--color-text-muted)";

function formatSyncedAgo(iso: string | null | undefined): string {
  if (!iso) return "Não configurado";
  const minutes = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 60000));
  if (minutes < 1) return "Sincronizado agora mesmo";
  return `Sincronizado há ${minutes} min`;
}

interface CategorySectionProps {
  title: string;
  count: number;
  children: ReactNode;
}

function CategorySection({ title, count, children }: CategorySectionProps) {
  return (
    <div className="mb-8">
      <div className="mb-3.5 flex items-baseline gap-2">
        <span className="text-[13px] font-bold text-text">{title}</span>
        <span className="text-xs text-text-muted">
          {count} {count === 1 ? "integração" : "integrações"}
        </span>
      </div>
      <div className="grid grid-cols-1 gap-5 md:grid-cols-2 xl:grid-cols-3">{children}</div>
    </div>
  );
}

function actionButton(canManage: boolean, connected: boolean, onClick: () => void): ReactNode {
  if (!canManage) return null;
  return (
    <Button type="button" variant={connected ? "secondary" : "primary"} className="w-full" onClick={onClick}>
      {connected ? "Editar conexão" : "Conectar"}
    </Button>
  );
}

function DatadogCard({ canManage, onConnect }: { canManage: boolean; onConnect: () => void }) {
  const { data, isLoading, isError } = useIntegrationStatus();
  const connected = data?.connected ?? false;
  const status: IntegrationStatusKind = connected ? "connected" : "not_connected";

  let meta = isLoading ? "Carregando…" : formatSyncedAgo(connected ? data?.last_checked_at : null);
  if (isError) meta = "Não foi possível carregar";

  return (
    <IntegrationCard
      icon={<MdOutlineShowChart size={24} color="var(--color-accent)" aria-hidden="true" />}
      bannerBg="#F3F1FB"
      status={status}
      title="Datadog"
      description="Métricas e logs dos serviços monitorados, com alertas sincronizados em tempo real."
      meta={meta}
      action={isLoading || isError ? null : actionButton(canManage, connected, onConnect)}
    />
  );
}

function NewRelicCard() {
  return (
    <IntegrationCard
      icon={<MdOutlineBarChart size={24} color={textMuted} aria-hidden="true" />}
      bannerBg={cardHeaderBg}
      status="coming_soon"
      title="New Relic"
      description="Monitoramento de performance de aplicações (APM) e infraestrutura."
    />
  );
}

function LLMProviderCard({ canManage, onConnect }: { canManage: boolean; onConnect: () => void }) {
  const { data, isLoading, isError } = useLLMProviders(1);
  const status = data?.providers.find((p) => p.provider === "openai");
  const connected = status?.status === "connected";
  const kind: IntegrationStatusKind = connected ? "connected" : "not_connected";

  let meta = isLoading ? "Carregando…" : connected ? `OpenAI · ${status?.model}` : "Não configurado";
  if (isError) meta = "Não foi possível carregar";

  return (
    <IntegrationCard
      icon={<MdOutlineSmartToy size={24} color="#B45309" aria-hidden="true" />}
      bannerBg="#FBF4E9"
      status={kind}
      title="LLM Provider"
      description="Geração de resumos e fechamento assistido de incidentes com IA."
      meta={meta}
      action={isLoading || isError ? null : actionButton(canManage, connected, onConnect)}
    />
  );
}

const EMAIL_PROVIDER_META: Record<
  EmailProviderName,
  { title: string; description: string; icon: ReactNode; bannerBg: string }
> = {
  resend: {
    title: "Resend",
    description: "Envio de alertas e notificações de incidentes para o time de plantão.",
    icon: <MdOutlineMailOutline size={24} color="#1A9E6B" aria-hidden="true" />,
    bannerBg: "#EEF6F1",
  },
  sendgrid: {
    title: "SendGrid",
    description: "Envio de alertas e notificações de incidentes por email.",
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
  const providerMeta = EMAIL_PROVIDER_META[id];
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
      setError(err instanceof ApiError ? err.message : `Não foi possível ativar o ${providerMeta.title}.`);
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
        icon={providerMeta.icon}
        bannerBg={providerMeta.bannerBg}
        status={kind}
        title={providerMeta.title}
        description={providerMeta.description}
        meta={isError ? "Não foi possível carregar" : connected ? (isActive ? "Ativo" : "Verificado") : "Não configurado"}
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
                      Ativar
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
              {actionButton(canManage, connected, onConnect)}
            </div>
          )
        }
      />
      <EmailDisconnectDialog
        open={disconnectDialogOpen}
        onOpenChange={setDisconnectDialogOpen}
        onConfirm={handleConfirmDisconnect}
        pending={disconnectMutation.isPending}
        providerLabel={providerMeta.title}
        t={t}
      />
    </>
  );
}

function EmailDisconnectDialog({
  open,
  onOpenChange,
  onConfirm,
  pending,
  providerLabel,
  t,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: () => void;
  pending: boolean;
  providerLabel: string;
  t: (key: string, opts?: Record<string, unknown>) => string;
}) {
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={t("emailProviders.disconnectDialog.title")}
      description={t("emailProviders.disconnectDialog.body", { provider: providerLabel })}
      footer={
        <>
          <Button type="button" variant="secondary" onClick={() => onOpenChange(false)}>
            {t("emailProviders.disconnectDialog.cancel")}
          </Button>
          <Button
            type="button"
            variant="solid"
            className="!border-critical !bg-critical hover:!bg-critical"
            onClick={onConfirm}
            disabled={pending}
          >
            {t("emailProviders.disconnectDialog.confirm")}
          </Button>
        </>
      }
    />
  );
}

export function IntegrationsPage() {
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
        <h2 className="text-text">Integrações</h2>
        <p className="m-0 text-[13.5px] text-text-muted">
          Conecte o Vane às ferramentas que alimentam o monitoramento e a análise de incidentes.
        </p>
      </div>

      <CategorySection title="APM & Observabilidade" count={2}>
        <DatadogCard canManage={canManage} onConnect={() => setDatadogDrawerOpen(true)} />
        <NewRelicCard />
      </CategorySection>

      <CategorySection title="IA" count={1}>
        <LLMProviderCard canManage={canManage} onConnect={() => setLlmDrawerOpen(true)} />
      </CategorySection>

      <CategorySection title="E-mail" count={2}>
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
