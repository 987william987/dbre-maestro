export type PlatformSettings = {
  sensitive_export_reviewer_user_ids: number[]
  sensitive_query_access_reviewer_user_ids: number[]
  db_metadata_inventory_enabled: boolean
  db_metadata_inventory_regions: string[]
  db_metadata_inventory_engines: string[]
  db_metadata_inventory_sync_interval_minutes: number
  db_metadata_object_enabled: boolean
  db_metadata_object_enabled_connection_ids: number[]
  db_metadata_object_sync_interval_minutes: number
}
