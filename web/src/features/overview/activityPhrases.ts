import type { AuditLogEntry } from "../../types/api";

// KNOWN_ACTIVITY_ACTIONS is every audit.Log action this codebase records
// today (recent-team-activity, context.md's action->phrase table, extended
// by audit-log-expansion's AUDITEXP-18 with 15 more entity actions). Each
// maps to an i18n key under overview.activity.actions.<action> - the
// RecentActivity component renders the actor's (bolded) name separately,
// same as the mock it replaces, so these phrases cover only the
// action+target clause, not the actor.
const KNOWN_ACTIVITY_ACTIONS = [
  "invited",
  "resent",
  "canceled",
  "role_changed",
  "removed",
  "domain_verified",
  "domain_deleted",
  "status_page_deleted",
  "status_page_domain_verified",
  "service_created",
  "service_updated",
  "service_deleted",
  "status_page_created",
  "status_page_domain_attached",
  "status_page_services_updated",
  "domain_created",
  "datadog_connected",
  "email_provider_connected",
  "email_provider_activated",
  "llm_provider_connected",
  "llm_provider_activated",
  "company_settings_updated",
  "company_logo_updated",
  "tenant_deleted",
] as const;

type KnownActivityAction = (typeof KNOWN_ACTIVITY_ACTIONS)[number];

function isKnownActivityAction(action: string): action is KnownActivityAction {
  return (KNOWN_ACTIVITY_ACTIONS as readonly string[]).includes(action);
}

// activityPhraseKey resolves the i18n key + interpolation values for one
// audit-log entry's action+target clause, per context.md's phrase table
// (ACTIVITY-10). An action not in KNOWN_ACTIVITY_ACTIONS (a future call
// site added without updating this map) falls back to the generic phrase
// instead of crashing or omitting the row. WHERE target_label is null (a
// historical pre-feature row) the "WithoutTarget" key variant is used,
// omitting the target clause gracefully instead of interpolating
// "null"/"undefined" (ACTIVITY-11).
export function activityPhraseKey(
  entry: Pick<AuditLogEntry, "action" | "target_label">,
): { key: string; values: Record<string, string> } {
  const target = entry.target_label;

  if (!isKnownActivityAction(entry.action)) {
    return target
      ? { key: "overview.activity.actions.unknown", values: { action: entry.action, target } }
      : { key: "overview.activity.actions.unknownWithoutTarget", values: { action: entry.action } };
  }

  return target
    ? { key: `overview.activity.actions.${entry.action}`, values: { target } }
    : { key: `overview.activity.actions.${entry.action}WithoutTarget`, values: {} };
}
