# Release Notes for v0.2.6


### Added

- LLM-powered SLO analysis: connect an OpenAI API key from the admin Integrations page (encrypted at rest, model selectable from a fixed allowlist — `gpt-4o-mini`/`gpt-4o`/`gpt-4.1-mini`/`gpt-4.1`) to enable three async enrichments, none of which ever block the poll cycle or a public-page request: a degraded service gets a short LLM-written analysis shown as a tooltip on its public status badge; a service transitioning to `outage` with no already-open incident gets one auto-created with an LLM-written description (falls back to a generic description on any LLM error/timeout); and once an auto-created incident's outage resolves, the LLM drafts a closing comment as a pending proposal an admin must explicitly confirm or discard from the incident detail view — never auto-published.


