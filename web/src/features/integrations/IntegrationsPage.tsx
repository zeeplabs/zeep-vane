import { useState, type ReactNode } from "react";
import {
  MdOutlineShowChart,
  MdOutlineBarChart,
  MdOutlineSmartToy,
  MdOutlineMailOutline,
  MdOutlineSend,
} from "react-icons/md";
import { Button } from "../../components/ui/Button";
import { useAuth } from "../../auth/AuthProvider";
import { IntegrationCard, type IntegrationStatusKind } from "./IntegrationCard";
import { ConnectDatadogDrawer } from "./ConnectDatadogDrawer";
import { ConnectLLMProviderDrawer } from "./ConnectLLMProviderDrawer";
import { ConnectEmailProviderDrawer } from "./ConnectEmailProviderDrawer";
import { useIntegrationStatus } from "./hooks";
import { useEmailProviders, type EmailProviderName, type EmailProviderStatus } from "../email-providers/hooks";
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
  const { data, isLoading } = useIntegrationStatus();
  const connected = data?.connected ?? false;
  const status: IntegrationStatusKind = connected ? "connected" : "not_connected";

  return (
    <IntegrationCard
      icon={<MdOutlineShowChart size={24} color="var(--color-accent)" aria-hidden="true" />}
      bannerBg="#F3F1FB"
      status={status}
      title="Datadog"
      description="Métricas e logs dos serviços monitorados, com alertas sincronizados em tempo real."
      meta={isLoading ? "Carregando…" : formatSyncedAgo(connected ? data?.last_checked_at : null)}
      action={isLoading ? null : actionButton(canManage, connected, onConnect)}
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
  const { data, isLoading } = useLLMProviders(1);
  const status = data?.providers.find((p) => p.provider === "openai");
  const connected = status?.status === "connected";
  const kind: IntegrationStatusKind = connected ? "connected" : "not_connected";

  return (
    <IntegrationCard
      icon={<MdOutlineSmartToy size={24} color="#B45309" aria-hidden="true" />}
      bannerBg="#FBF4E9"
      status={kind}
      title="LLM Provider"
      description="Geração de resumos e fechamento assistido de incidentes com IA."
      meta={isLoading ? "Carregando…" : connected ? `OpenAI · ${status?.model}` : "Não configurado"}
      action={isLoading ? null : actionButton(canManage, connected, onConnect)}
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
  canManage,
  onConnect,
}: {
  id: EmailProviderName;
  status?: EmailProviderStatus;
  canManage: boolean;
  onConnect: () => void;
}) {
  const meta = EMAIL_PROVIDER_META[id];
  const connected = status?.status === "connected";
  const kind: IntegrationStatusKind = connected ? "connected" : "not_connected";

  return (
    <IntegrationCard
      icon={meta.icon}
      bannerBg={meta.bannerBg}
      status={kind}
      title={meta.title}
      description={meta.description}
      meta={connected ? "Verificado" : "Não configurado"}
      action={actionButton(canManage, connected, onConnect)}
    />
  );
}

export function IntegrationsPage() {
  const { hasRole } = useAuth();
  const canManage = hasRole(["owner", "operator"]);
  const { data: emailData } = useEmailProviders(1);
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
          canManage={canManage}
          onConnect={() => setEmailDrawerProvider("resend")}
        />
        <EmailProviderCard
          id="sendgrid"
          status={byProvider.get("sendgrid")}
          canManage={canManage}
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
