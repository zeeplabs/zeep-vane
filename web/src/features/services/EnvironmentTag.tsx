import { useTranslation } from "react-i18next";
import { Tag } from "../../components/ui/Tag";
import type { ServiceEnvironment } from "../../types/api";
import { environmentLabel, environmentVariant } from "./environmentMeta";

export interface EnvironmentTagProps {
  environment: ServiceEnvironment;
}

/** Visual-only environment badge next to a service's name
 * (service-environment-tag SET-05): development=gray, staging=amber,
 * production=green. Renders nothing for "" (no tag) - admin-only, never
 * shown on the public status page (SET feature's own scope). */
export function EnvironmentTag({ environment }: EnvironmentTagProps) {
  const { t } = useTranslation();
  // Falsy guard (not just === ""): several test fixtures/mocks predate this
  // field and omit it entirely (undefined at runtime despite the required
  // ServiceEnvironment type) - treat that exactly like "" (no tag), never
  // throw on an unmapped environmentVariant lookup.
  if (!environment) {
    return null;
  }
  return (
    <Tag data-testid="environment-badge" variant={environmentVariant[environment]}>
      {environmentLabel(t, environment)}
    </Tag>
  );
}
