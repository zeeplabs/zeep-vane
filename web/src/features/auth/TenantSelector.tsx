import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Button } from "../../components/ui/Button";
import { useAuth } from "../../auth/AuthProvider";
import { ApiError } from "../../lib/apiClient";
import type { Role } from "../../types/api";

// TenantSelector is shown only when the authenticated user has more than 1
// tenant_membership and hasn't picked an active tenant yet
// (useAuth().needsTenantSelection, gated by App.tsx's route guard) - a
// consultant with access to more than one tenant (P2, TENANT-19/20/21).
// The every-day self-hosted case (exactly 1 membership) never reaches this
// screen at all.
export function TenantSelector() {
  const { t } = useTranslation();
  const { admin, switchTenant } = useAuth();
  const navigate = useNavigate();

  const [error, setError] = useState<string | null>(null);
  const [submittingId, setSubmittingId] = useState<string | null>(null);

  const memberships = admin?.memberships ?? [];

  const roleLabel: Record<Role, string> = {
    owner: t("tenantSelector.roleOwner"),
    operator: t("tenantSelector.roleOperator"),
    viewer: t("tenantSelector.roleViewer"),
  };

  async function handleSelect(tenantId: string) {
    setError(null);
    setSubmittingId(tenantId);
    try {
      await switchTenant(tenantId);
      navigate("/");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : t("tenantSelector.genericError"));
      setSubmittingId(null);
    }
  }

  return (
    <div className="flex min-h-screen w-full items-center justify-center bg-bg px-4">
      <div className="w-full max-w-[420px]">
        <div className="mb-7">
          <h3 className="text-text">{t("tenantSelector.title")}</h3>
          <p className="mt-1 text-[13.5px] text-neutral-400">{t("tenantSelector.subtitle")}</p>
        </div>

        {error ? (
          <p role="alert" className="mb-4 text-xs text-critical">
            {error}
          </p>
        ) : null}

        <ul className="flex flex-col gap-2">
          {memberships.map((m) => (
            <li key={m.tenant_id}>
              <Button
                type="button"
                variant="secondary"
                className="w-full justify-between"
                disabled={submittingId !== null}
                onClick={() => handleSelect(m.tenant_id)}
              >
                {/* Fallback to tenant_id for a legacy/edge-case row missing
                    name (spec.md Edge Case, new-layout-migration SHELL-20). */}
                <span>{m.name || m.tenant_id}</span>
                <span className="text-neutral-400">{roleLabel[m.role]}</span>
              </Button>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}
