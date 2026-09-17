## What's new in v0.4.1

### Changed

- **AI-generated status page text is now in Portuguese, in plain language.** The three AI-written texts on a public status page — a degraded-service tooltip, an incident description, and an incident closing comment — were previously written in English with technical wording (SLO, SLI, error budget). They're now written in Portuguese, in plain language aimed at a non-technical reader (e.g. HR, a manager), describing the practical impact instead of internal metrics.

### Upgrade notes

No breaking changes, no manual migration steps, no config changes required. This only affects newly generated AI text going forward — existing incident descriptions and closing comments already stored are not rewritten.
