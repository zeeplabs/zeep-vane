ALTER TABLE status_page_services
    DROP COLUMN group_id,
    DROP COLUMN parent_service_id,
    DROP COLUMN position;

DROP TABLE status_page_service_groups;
