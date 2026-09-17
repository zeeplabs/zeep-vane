import { describe, it, expect } from "vitest";
import i18n from "../../lib/i18n";
import { activityPhraseKey } from "./activityPhrases";

describe("activityPhraseKey", () => {
  // ACTIVITY-10: each of the 8 known actions renders its exact pt-BR
  // phrase (context.md's action->phrase table), interpolating target_label.
  it.each([
    ["invited", "novo@acme.health", "convidou novo@acme.health"],
    ["resent", "novo@acme.health", "reenviou convite para novo@acme.health"],
    ["canceled", "novo@acme.health", "cancelou convite de novo@acme.health"],
    ["role_changed", "Diego Rocha", "alterou o papel de Diego Rocha"],
    ["removed", "Diego Rocha", "removeu Diego Rocha"],
    ["domain_verified", "painel.acme.health", "verificou o domínio painel.acme.health"],
    ["domain_deleted", "painel.acme.health", "excluiu o domínio painel.acme.health"],
    ["status_page_deleted", "Status Acme", "excluiu a página Status Acme"],
    [
      "status_page_domain_verified",
      "Status Acme",
      "verificou o domínio da página Status Acme",
    ],
  ])("renders the exact phrase for %s", (action, target, expected) => {
    const { key, values } = activityPhraseKey({ action, target_label: target });
    expect(i18n.t(key, values)).toBe(expected);
  });

  // ACTIVITY-10 edge case: an action string not in the map renders the
  // generic fallback phrase instead of crashing or omitting the row.
  it("falls back to the generic phrase for an unrecognized action", () => {
    const { key, values } = activityPhraseKey({
      action: "future_action",
      target_label: "algo",
    });
    expect(i18n.t(key, values)).toBe("realizou future_action em algo");
  });

  // ACTIVITY-11: a null target_label (historical pre-feature row) renders
  // gracefully, omitting the target clause - never the literal
  // "null"/"undefined" string.
  it("omits the target clause gracefully when target_label is null", () => {
    const { key, values } = activityPhraseKey({ action: "role_changed", target_label: null });
    const rendered = i18n.t(key, values);
    expect(rendered).toBe("alterou um papel");
    expect(rendered).not.toContain("null");
    expect(rendered).not.toContain("undefined");
  });

  // Same null-target graceful omission for the unknown-action fallback.
  it("omits the target clause gracefully for an unknown action with a null target_label", () => {
    const { key, values } = activityPhraseKey({ action: "future_action", target_label: null });
    const rendered = i18n.t(key, values);
    expect(rendered).toBe("realizou future_action");
    expect(rendered).not.toContain("null");
    expect(rendered).not.toContain("undefined");
  });
});
