ALTER TABLE services ADD COLUMN slo_type TEXT;
ALTER TABLE services ADD COLUMN datadog_service_tag TEXT;
ALTER TABLE llm_settings ADD COLUMN root_cause_enrichment_enabled BOOLEAN NOT NULL DEFAULT false;
