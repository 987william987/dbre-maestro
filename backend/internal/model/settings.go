package model

type PlatformSettings struct {
	SensitiveExportReviewerUserIDs       []uint64 `json:"sensitive_export_reviewer_user_ids"`
	SensitiveQueryAccessReviewerUserIDs  []uint64 `json:"sensitive_query_access_reviewer_user_ids"`
	DBMetadataInventoryEnabled           bool     `json:"db_metadata_inventory_enabled"`
	DBMetadataInventoryRegions           []string `json:"db_metadata_inventory_regions"`
	DBMetadataInventoryEngines           []string `json:"db_metadata_inventory_engines"`
	DBMetadataInventorySyncIntervalMins  int      `json:"db_metadata_inventory_sync_interval_minutes"`
	DBMetadataObjectEnabled              bool     `json:"db_metadata_object_enabled"`
	DBMetadataObjectEnabledConnectionIDs []uint64 `json:"db_metadata_object_enabled_connection_ids"`
	DBMetadataObjectSyncIntervalMins     int      `json:"db_metadata_object_sync_interval_minutes"`
}
