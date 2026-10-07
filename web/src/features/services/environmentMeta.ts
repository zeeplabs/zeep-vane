import type { TagVariant } from "../../components/ui/Tag";
import type { ServiceEnvironment } from "../../types/api";

type Translator = (key: string, options?: Record<string, unknown>) => string;

// Shared environment-tag badge metadata (service-environment-tag SET-05):
// development=gray, staging=amber/yellow, production=green, ""=no badge at
// all (EnvironmentTag renders null for "") - same single-source-of-truth
// pattern statusMeta.ts already uses for ServiceStatus's badge.
export function environmentLabel(t: Translator, environment: ServiceEnvironment): string {
  const key = environment === "" ? "none" : environment;
  return t(`services.environment.${key}`);
}

// environmentVariant only has entries for the three real tag values -
// EnvironmentTag never looks this up for "" (it renders nothing instead).
export const environmentVariant: Record<Exclude<ServiceEnvironment, "">, TagVariant> = {
  development: "neutral",
  staging: "warning",
  production: "success",
};

// environmentOptions drives the create/edit form's <select> (SET-05):
// "" first (the default, "no tag"), in the same order badges read.
export const environmentOptions: ServiceEnvironment[] = ["", "development", "staging", "production"];
