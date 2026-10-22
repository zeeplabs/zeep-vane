# v0.11.0

## Changed

**Service status now follows Datadog's burn rate, not a noisy 5-minute SLI.** Vane used to compare a 5-minute SLI against the SLO's 30-day target, map Datadog's `no_data` to `degraded`, and freeze status on low traffic — so a service could stay degraded/outage for hours after Datadog already showed it healthy. The poller now reads a 5-minute and a 1-hour window from one Datadog `/history` call and classifies each service by multi-window burn rate, the same signal Datadog's `slo alert` monitors use:

- `outage` when both windows burn the error budget at ≥ 14.4x, `degraded` at ≥ 7.2x, and only with at least 3 failed requests in the last 5 minutes (1-2 isolated errors never change status);
- recovery as soon as the last 5 minutes are clean;
- a service with no traffic keeps its status for 15 minutes after its last error (persisted, survives restarts), then returns to operational;
- monitor-based SLOs and SLOs without a target keep the previous state-based classification.

A 7-day replay against real production SLOs went from 102 false status episodes to 0, with recovery in the same cycle as Datadog's own monitor.

**Transactional emails redesigned.** Invite, password reset, signup verification, incident opened, incident resolved and the weekly digest use new email-client-safe designs with a hosted logo and a plain-text alternative. Copy is in English; dates use the tenant's timezone.

**Weekly digest per tenant timezone.** The digest is sent at about 09:00 Monday in each tenant's own timezone (at most once per week) and covers the previous local calendar week. It lists up to 10 incidents (open ones first) with a "+N more" link.

**LLM text.** Outage descriptions and closing comments use the technical-but-accessible tone of the degraded tooltip and now mention the recent error rate and burn rate. Auto-detected incident titles are in pt-BR.

## Added

- **Latest reading** in the admin service detail: error rate of the last 5 minutes, burn rate over 5 minutes and 1 hour, and when it was read. Never exposed on the public status page.
- **Closing note on manual resolve**: an optional note (up to 2,000 characters) added to the timeline and used in the "Resolved" email.
- Debug-level log of each poll cycle's classification inputs.

## Fixed

- Manual incident resolve/close now runs inside the tenant's row-level-security scope.
- Resolving an already-resolved incident no longer re-sends the "Resolved" email or duplicates timeline entries.
- A closing note typed on one incident could be sent with another incident's resolve.
- Service detail/list API returned an empty `monitor_mode`.

## Upgrading

- Migrations `0051` (`tenants.last_digest_sent_at`) and `0052` (six nullable "latest reading" columns on `services`) run automatically on startup. No backfill; the first poll cycle fills the readings.
- New optional environment variable `VANE_EMAIL_ASSETS_BASE_URL`: base URL emails load their logo from. The default (admin host + `/email`) works when the admin host is reachable from the internet; set it to a public URL (e.g. a CDN) when it is not, or emails show the "Vane" wordmark without the image.
- Services whose SLO was linked before the SLO type was recorded (empty `slo_type`) keep the previous state-based classification when Datadog returns no request counts. To move one to burn-rate classification in that case, set its type with `PATCH /api/services/{id}` and `"slo_type": "metric"`, or recreate the service.
- Expect more status transitions on services that hover around 7.2x burn (the same flapping Datadog's own monitors show); incidents still open only on `outage`.
