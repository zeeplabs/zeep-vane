## Fixed

- **False "outage" for low-traffic services.** A service whose Datadog SLO receives zero requests during a 5-minute poll window could be classified as `outage` — the breach-detection logic compared a data-less window's SLI (defaulting to 0) against the breach threshold, which always reads as a breach regardless of real health. Two consecutive zero-request windows were enough to latch a healthy, simply low-traffic service into a permanent false outage on the public status page, since the same low-volume guard that would normally carry the previous status forward then kept re-confirming the wrong value.
- Any service already stuck in a false `outage`/`degraded` state from this gap self-corrects automatically on its first poll after upgrading — no manual intervention or database change needed.

This does not affect services with real traffic, or genuine outages detected from actual failed requests (a failing request still counts toward a window's request volume, so this fix never masks a real breach).

## Upgrade notes

No migration, no config change. Standard rolling upgrade.
