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
export function failureMessage(providers: string[]): string {
  const labels = providers.map(providerLabel);
  if (labels.length === 1) {
    return `Falha ao verificar a integração ${labels[0]} — última tentativa não teve sucesso.`;
  }
  return `Falha ao verificar as integrações ${labels.join(" e ")} — última tentativa não teve sucesso.`;
}
