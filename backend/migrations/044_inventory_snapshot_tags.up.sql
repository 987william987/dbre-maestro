ALTER TABLE cloud_db_inventory_snapshots
    ADD COLUMN tags_json JSON NULL AFTER raw_payload_json;
