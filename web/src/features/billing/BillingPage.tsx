// BillingPage: decorative showcase for Planos & Faturamento (billing-plans-
// page, BILLPG-01..06). Neither Stripe nor zeep-license-server is integrated
// today (AD-025 already paused any real plan/license enforcement pending a
// cross-repo decision) - every mutating action here (upgrade/downgrade,
// buy/activate a license) shows a "coming soon" toast and never sends a
// request or renders a card-number input, matching the same decorative
// pattern already used for OAuthButtons and the New Relic integration card.

import { toast } from "sonner";
import { useTranslation } from "react-i18next";
import { useAuth } from "../../auth/AuthProvider";
import { Card } from "../../components/ui/Card";
import { Button } from "../../components/ui/Button";

type Translator = (key: string, options?: Record<string, unknown>) => string;

interface PlanDef {
  key: "free" | "starter" | "scale";
  price: string;
  recommended?: boolean;
}

const PLAN_DEFS: PlanDef[] = [
  { key: "free", price: "R$ 0" },
  { key: "starter", price: "R$ 49", recommended: true },
  { key: "scale", price: "R$ 149" },
];

const LICENSE_PRICE = "R$ 1.490";

function planNameFor(planTier: string, t: Translator): string {
  if (!planTier) return t("billing.plans.free.name");
  const known = PLAN_DEFS.find((p) => p.key === planTier);
  return known ? t(`billing.plans.${known.key}.name`) : planTier;
}

function showComingSoon(t: Translator) {
  toast.info(t("billing.comingSoon"));
}

function PlanCard({ plan, isCurrent, t }: { plan: PlanDef; isCurrent: boolean; t: Translator }) {
  const name = t(`billing.plans.${plan.key}.name`);
  const tagline = t(`billing.plans.${plan.key}.tagline`);
  const features = t(`billing.plans.${plan.key}.features`, { returnObjects: true }) as unknown as string[];

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
          {t("billing.recommended")}
        </span>
      ) : null}
      <div>
        <p className="m-0 text-[15px] font-bold text-text">{name}</p>
        <p className="m-0 mt-1 min-h-8 text-[12.5px] leading-relaxed text-neutral-400">{tagline}</p>
      </div>
      <div className="flex items-baseline gap-1">
        <span className="text-[22px] font-bold text-text">{plan.price}</span>
        {plan.key !== "free" ? <span className="text-[13px] text-neutral-400">{t("billing.perMonth")}</span> : null}
      </div>
      <div className="h-px bg-divider" />
      <ul className="flex flex-1 flex-col gap-2.5">
        {features.map((feat) => (
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
        {isCurrent
          ? t("billing.currentPlanButton")
          : plan.key === "free"
            ? t("billing.downgradeButton")
            : t("billing.upgradeButton")}
      </Button>
    </Card>
  );
}

function SaasBilling({ planTier, t }: { planTier: string; t: Translator }) {
  const isFree = !planTier || planTier === "free";
  const currentPlanName = planNameFor(planTier, t);

  return (
    <>
      <div className="mb-7 flex items-center justify-between gap-4 rounded-md border border-divider bg-card-header-bg p-[18px_20px]">
        <div>
          <p className="m-0 text-[13px] text-neutral-400">{t("billing.currentPlan")}</p>
          <p className="m-0 text-base font-bold text-text">{currentPlanName}</p>
        </div>
        <p className="m-0 text-[12.5px] text-neutral-400">
          {isFree ? t("billing.noRecurringCharge") : t("billing.monthlyBilling")}
        </p>
      </div>

      <div className="mb-8 grid grid-cols-3 gap-[18px]">
        {PLAN_DEFS.map((plan) => (
          <PlanCard
            key={plan.key}
            plan={plan}
            isCurrent={planNameFor(planTier, t) === t(`billing.plans.${plan.key}.name`)}
            t={t}
          />
        ))}
      </div>

      <div className="grid grid-cols-2 gap-[18px]">
        <Card elevation="none" className="border border-divider p-5">
          <p className="m-0 text-[13.5px] font-bold text-text">{t("billing.paymentMethod.title")}</p>
          <p className="m-0 mt-3.5 text-[12.5px] leading-relaxed text-neutral-400">
            {t("billing.paymentMethod.empty")}
          </p>
        </Card>
        <Card elevation="none" className="border border-divider p-5">
          <p className="m-0 text-[13.5px] font-bold text-text">{t("billing.invoices.title")}</p>
          <p className="m-0 mt-3.5 text-[12.5px] leading-relaxed text-neutral-400">{t("billing.invoices.empty")}</p>
        </Card>
      </div>
    </>
  );
}

function SelfHostedBilling({ t }: { t: Translator }) {
  const licenseName = t("billing.license.name");
  const licenseFeatures = t("billing.license.features", { returnObjects: true }) as unknown as string[];

  return (
    <>
      <div className="mb-7 flex items-center justify-between gap-4 rounded-md border border-warning/30 bg-warning/10 p-[18px_20px]">
        <div>
          <p className="m-0 text-[13px] text-neutral-400">{t("billing.selfHosted.title")}</p>
          <p className="m-0 text-base font-bold text-text">{t("billing.selfHosted.noActiveLicense")}</p>
        </div>
        <p className="m-0 text-[12.5px] text-neutral-400">{t("billing.selfHosted.defaultFeaturesLocked")}</p>
      </div>

      <div className="grid grid-cols-2 gap-[18px]">
        <Card elevation="none" className="flex flex-col gap-4 border border-divider p-[24px]">
          <div>
            <p className="m-0 text-[15px] font-bold text-text">{licenseName}</p>
            <p className="m-0 mt-1 text-[12.5px] leading-relaxed text-neutral-400">
              {t("billing.selfHosted.licenseDescription")}
            </p>
          </div>
          <div className="flex items-baseline gap-1">
            <span className="text-[22px] font-bold text-text">{LICENSE_PRICE}</span>
            <span className="text-[13px] text-neutral-400">{t("billing.selfHosted.perYear")}</span>
          </div>
          <div className="h-px bg-divider" />
          <ul className="flex flex-1 flex-col gap-2.5">
            {licenseFeatures.map((feat) => (
              <li key={feat} className="flex items-start gap-2 text-[12.5px] leading-relaxed text-neutral-400">
                <span aria-hidden="true" className="mt-0.5 text-success">
                  ✓
                </span>
                {feat}
              </li>
            ))}
          </ul>
          <Button variant="solid" onClick={() => showComingSoon(t)} data-testid="license-buy-button">
            {t("billing.selfHosted.buyLicenseButton")}
          </Button>
        </Card>

        <Card elevation="none" className="flex flex-col gap-3 border border-divider p-[24px]">
          <p className="m-0 text-[15px] font-bold text-text">{t("billing.selfHosted.activateTitle")}</p>
          <p className="m-0 text-[12.5px] leading-relaxed text-neutral-400">
            {t("billing.selfHosted.activateDescription")}
          </p>
          <label className="text-[12.5px] font-semibold text-neutral-400" htmlFor="license-key-input">
            {t("billing.selfHosted.licenseKeyLabel")}
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
            {t("billing.selfHosted.activateButton")}
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
