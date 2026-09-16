// BillingPage: decorative showcase for Planos & Faturamento (billing-plans-
// page, BILLPG-01..06). Neither Stripe nor zeep-license-server is integrated
// today (AD-025 already paused any real plan/license enforcement pending a
// cross-repo decision) - every mutating action here (upgrade/downgrade,
// buy/activate a license) shows a "coming soon" toast and never sends a
// request or renders a card-number input, matching the same decorative
// pattern already used for OAuthButtons and the New Relic integration card.
// Plan/license copy (name/tagline/price/features) is ported verbatim from
// the mock's own PLAN_DEFS/LICENSE_DEF as static PT-BR content, not routed
// through i18n - it's real marketing copy the mock itself never localizes,
// same precedent as PublicStatusPage's rangeAgoLabel/hourlyLabel maps.

import { toast } from "sonner";
import { useTranslation } from "react-i18next";
import { useAuth } from "../../auth/AuthProvider";
import { Card } from "../../components/ui/Card";
import { Button } from "../../components/ui/Button";

interface PlanDef {
  key: "free" | "starter" | "scale";
  name: string;
  tagline: string;
  price: string;
  recommended?: boolean;
  features: string[];
}

const PLAN_DEFS: PlanDef[] = [
  {
    key: "free",
    name: "Free",
    tagline: "Para começar a monitorar o essencial.",
    price: "R$ 0",
    features: [
      "Até 3 usuários",
      "1 status page pública",
      "Histórico de 7 dias",
      "Polling manual ilimitado",
      "Suporte via comunidade",
    ],
  },
  {
    key: "starter",
    name: "Starter",
    tagline: "Para times que já dependem do Vane no dia a dia.",
    price: "R$ 49",
    recommended: true,
    features: [
      "Até 10 usuários",
      "Até 3 status pages",
      "1 domínio próprio",
      "Fechamento de incidentes com IA",
      "Histórico de 30 dias",
      "Suporte por email",
    ],
  },
  {
    key: "scale",
    name: "Scale",
    tagline: "Para operações críticas em múltiplos times.",
    price: "R$ 149",
    features: [
      "Usuários ilimitados",
      "Status pages ilimitadas",
      "Domínios próprios ilimitados",
      "Fechamento de incidentes com IA",
      "Histórico de 90 dias",
      "SSO e suporte prioritário",
    ],
  },
];

const LICENSE_DEF = {
  name: "Licença Anual",
  price: "R$ 1.490",
  features: [
    "Usuários ilimitados",
    "Domínios próprios e status pages ilimitados",
    "Fechamento de incidentes com IA",
    "SSO e suporte prioritário",
    "Atualizações durante a vigência da licença",
  ],
};

function planNameFor(planTier: string): string {
  if (!planTier) return "Free";
  const known = PLAN_DEFS.find((p) => p.key === planTier);
  return known ? known.name : planTier;
}

function showComingSoon(t: (key: string) => string) {
  toast.info(t("billing.comingSoon"));
}

function PlanCard({ plan, isCurrent, t }: { plan: PlanDef; isCurrent: boolean; t: (key: string) => string }) {
  return (
    <Card
      elevation="none"
      data-testid={`plan-card-${plan.key}`}
      className={
        "relative flex flex-col gap-4 rounded-md border p-[24px] " +
        (isCurrent ? "border-accent" : plan.recommended ? "border-accent-300" : "border-divider")
      }
    >
      {plan.recommended && !isCurrent ? (
        <span className="absolute -top-2.5 left-5 rounded-full bg-accent px-2.5 py-0.5 text-[10.5px] font-bold text-white">
          Recomendado
        </span>
      ) : null}
      <div>
        <p className="m-0 text-[15px] font-bold text-text">{plan.name}</p>
        <p className="m-0 mt-1 min-h-8 text-[12.5px] leading-relaxed text-neutral-400">{plan.tagline}</p>
      </div>
      <div className="flex items-baseline gap-1">
        <span className="text-[22px] font-bold text-text">{plan.price}</span>
        {plan.key !== "free" ? <span className="text-[13px] text-neutral-400">/mês</span> : null}
      </div>
      <div className="h-px bg-divider" />
      <ul className="flex flex-1 flex-col gap-2.5">
        {plan.features.map((feat) => (
          <li key={feat} className="flex items-start gap-2 text-[12.5px] leading-relaxed text-neutral-400">
            <span aria-hidden="true" className="mt-0.5 text-success">
              ✓
            </span>
            {feat}
          </li>
        ))}
      </ul>
      <Button
        variant={isCurrent ? "secondary" : plan.key === "free" ? "secondary" : "solid"}
        disabled={isCurrent}
        onClick={() => showComingSoon(t)}
        data-testid={`plan-card-${plan.key}-button`}
      >
        {isCurrent ? "Plano atual" : plan.key === "free" ? "Fazer downgrade" : "Fazer upgrade"}
      </Button>
    </Card>
  );
}

function SaasBilling({ planTier, t }: { planTier: string; t: (key: string) => string }) {
  const isFree = !planTier || planTier === "free";
  const currentPlanName = planNameFor(planTier);

  return (
    <>
      <div className="mb-7 flex items-center justify-between gap-4 rounded-md border border-divider bg-card-header-bg p-[18px_20px]">
        <div>
          <p className="m-0 text-[13px] text-neutral-400">Plano atual</p>
          <p className="m-0 text-base font-bold text-text">{currentPlanName}</p>
        </div>
        <p className="m-0 text-[12.5px] text-neutral-400">
          {isFree ? "Sem cobrança recorrente" : "Cobrança mensal"}
        </p>
      </div>

      <div className="mb-8 grid grid-cols-3 gap-[18px]">
        {PLAN_DEFS.map((plan) => (
          <PlanCard key={plan.key} plan={plan} isCurrent={planNameFor(planTier) === plan.name} t={t} />
        ))}
      </div>

      <div className="grid grid-cols-2 gap-[18px]">
        <Card elevation="none" className="border border-divider p-5">
          <p className="m-0 text-[13.5px] font-bold text-text">Forma de pagamento</p>
          <p className="m-0 mt-3.5 text-[12.5px] leading-relaxed text-neutral-400">
            Nenhum cartão cadastrado. Um cartão é adicionado ao fazer upgrade para um plano pago.
          </p>
        </Card>
        <Card elevation="none" className="border border-divider p-5">
          <p className="m-0 text-[13.5px] font-bold text-text">Faturas</p>
          <p className="m-0 mt-3.5 text-[12.5px] leading-relaxed text-neutral-400">
            Suas faturas aparecerão aqui após a primeira cobrança da assinatura.
          </p>
        </Card>
      </div>
    </>
  );
}

function SelfHostedBilling({ t }: { t: (key: string) => string }) {
  return (
    <>
      <div className="mb-7 flex items-center justify-between gap-4 rounded-md border border-warning/30 bg-warning/10 p-[18px_20px]">
        <div>
          <p className="m-0 text-[13px] text-neutral-400">Licença self-hosted</p>
          <p className="m-0 text-base font-bold text-text">Nenhuma licença ativa</p>
        </div>
        <p className="m-0 text-[12.5px] text-neutral-400">Recursos padrão bloqueados</p>
      </div>

      <div className="grid grid-cols-2 gap-[18px]">
        <Card elevation="none" className="flex flex-col gap-4 border border-divider p-[24px]">
          <div>
            <p className="m-0 text-[15px] font-bold text-text">{LICENSE_DEF.name}</p>
            <p className="m-0 mt-1 text-[12.5px] leading-relaxed text-neutral-400">
              Compra única, licença válida por 12 meses no seu ambiente self-hosted.
            </p>
          </div>
          <div className="flex items-baseline gap-1">
            <span className="text-[22px] font-bold text-text">{LICENSE_DEF.price}</span>
            <span className="text-[13px] text-neutral-400">/ano</span>
          </div>
          <div className="h-px bg-divider" />
          <ul className="flex flex-1 flex-col gap-2.5">
            {LICENSE_DEF.features.map((feat) => (
              <li key={feat} className="flex items-start gap-2 text-[12.5px] leading-relaxed text-neutral-400">
                <span aria-hidden="true" className="mt-0.5 text-success">
                  ✓
                </span>
                {feat}
              </li>
            ))}
          </ul>
          <Button variant="solid" onClick={() => showComingSoon(t)} data-testid="license-buy-button">
            Comprar licença
          </Button>
        </Card>

        <Card elevation="none" className="flex flex-col gap-3 border border-divider p-[24px]">
          <p className="m-0 text-[15px] font-bold text-text">Ativar licença</p>
          <p className="m-0 text-[12.5px] leading-relaxed text-neutral-400">
            Após a compra, enviamos um código de licença por email. Cole o código abaixo para desbloquear os recursos
            padrão nesta instância.
          </p>
          <label className="text-[12.5px] font-semibold text-neutral-400" htmlFor="license-key-input">
            Código de licença
          </label>
          <input
            id="license-key-input"
            type="text"
            placeholder="VANE-XXXX-XXXX-XXXX"
            disabled
            className="w-full rounded-md border border-transparent bg-card-header-bg px-3 py-2.5 font-mono text-sm text-text"
          />
          <div className="flex-1" />
          <Button variant="secondary" onClick={() => showComingSoon(t)} data-testid="license-activate-button">
            Ativar licença
          </Button>
        </Card>
      </div>
    </>
  );
}

export function BillingPage() {
  const { t } = useTranslation();
  const { admin } = useAuth();
  const memberships = admin?.memberships ?? [];
  const active = memberships.find((m) => m.tenant_id === admin?.active_tenant_id) ?? memberships[0];
  const planTier = active?.plan_tier ?? "";
  const { deploymentMode } = useAuth();

  return (
    <div className="flex flex-col gap-6">
      <div>
        <h1 className="m-0 mb-1.5 text-xl font-bold tracking-tight text-text">{t("billing.title")}</h1>
        <p className="m-0 max-w-xl text-[13.5px] leading-relaxed text-neutral-400">{t("billing.subtitle")}</p>
      </div>

      {deploymentMode === "saas" ? (
        <SaasBilling planTier={planTier} t={t} />
      ) : (
        <SelfHostedBilling t={t} />
      )}
    </div>
  );
}
