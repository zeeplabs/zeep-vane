-- status_page_service_groups (migration 0046) has no index on
-- status_page_id, despite every read of this table (ListGroups,
-- validGroupIDsTx, and the group LEFT JOIN in ServiceRepository.
-- ListForStatusPage) filtering or joining on it - every one of those
-- queries has been doing a sequential scan of the whole table. Harmless at
-- today's data volume, but it's the kind of gap that only shows up as a
-- real production slowdown once there are enough status pages/groups
-- across tenants to matter, and by then it's a live-site index build
-- instead of a routine migration.
CREATE INDEX status_page_service_groups_status_page_id_idx
    ON status_page_service_groups (status_page_id);
