type Translator = (key: string, options?: Record<string, unknown>) => string;

// replicaLabel maps the backend's replicaApplicationName() fallback
// (internal/cli/poller_manager.go, when HOSTNAME is unset - local dev,
// self-hosted without Kubernetes) to a human-readable label. Kubernetes
// always sets HOSTNAME to the pod name, so "unknown" only ever surfaces
// outside that environment.
export function replicaLabel(t: Translator, applicationName: string): string {
  return applicationName === "unknown" ? t("poller.localReplica") : applicationName;
}

const PROVIDER_LABELS: Record<string, string> = {
  datadog: "Datadog",
  sendgrid: "SendGrid",
  resend: "Resend",
};

export function providerLabel(provider: string): string {
  return PROVIDER_LABELS[provider] ?? provider;
}

// Names the specific integration(s) so an operator doesn't have to open the
// details page just to know which credential to rotate.
export function failureMessage(t: Translator, providers: string[]): string {
  const labels = providers.map(providerLabel);
  if (labels.length === 1) {
    return t("poller.failureMessage.single", { provider: labels[0] });
  }
  return t("poller.failureMessage.multiple", { providers: labels.join(t("poller.providersSeparator")) });
}
