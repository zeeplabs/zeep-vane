## Fixed

- **False "degraded" for low-traffic services (extends v0.5.1's fix).** A service just linked to a healthy Datadog SLO could show `degraded` on the public status page while its traffic was still ramping up. v0.5.1 fixed this for the exact-zero-request edge case; this release extends the same fix to the full low-traffic range (1-9 requests in a 5-minute poll window). With that few requests, a single failed one swings the window's own SLI far enough to trip the breach comparison, even though Datadog's own aggregated SLO state reports the service healthy. A service's first-ever classification now trusts Datadog's state across the whole low-volume range instead of only the zero-request edge.
- Any service already showing a false `degraded`/`outage` from this gap self-corrects automatically on its first poll after upgrading — no manual intervention or database change needed.

This does not affect services with real traffic, or genuine degradation/outages detected from actual failed requests.

## Upgrade notes

No migration, no config change. Standard rolling upgrade.
