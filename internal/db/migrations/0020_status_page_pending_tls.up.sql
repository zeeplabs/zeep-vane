-- One-time data fix: AttachDomain used to leave state at "draft" after
-- setting domain_id/subdomain, but tls.HostPolicy rejects ACME issuance
-- for any hostname whose status page is still "draft" - a deadlock no
-- status page could ever escape (MarkPublished/MarkTLSFailed are only
-- reachable via a HostPolicy-approved ACME attempt). AttachDomain now
-- sets state to "pending_tls" going forward; this backfills any row
-- already stuck in the unreachable draft+domain-attached combination.
UPDATE status_pages SET state = 'pending_tls' WHERE state = 'draft' AND domain_id IS NOT NULL;
