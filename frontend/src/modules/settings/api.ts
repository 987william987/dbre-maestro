import { apiClient } from '@/shared/api/client'
import type { DBConnection } from '@/shared/types/dbConnection'
import type { PlatformSettings } from '@/shared/types/settings'

function normalizeSettings(settings: PlatformSettings): PlatformSettings {
  return {
    sensitive_export_reviewer_user_ids: Array.isArray(settings.sensitive_export_reviewer_user_ids)
      ? settings.sensitive_export_reviewer_user_ids
      : [],
    sensitive_query_access_reviewer_user_ids: Array.isArray(settings.sensitive_query_access_reviewer_user_ids)
      ? settings.sensitive_query_access_reviewer_user_ids
      : [],
    require_non_sensitive_export_review: typeof settings.require_non_sensitive_export_review === 'boolean'
      ? settings.require_non_sensitive_export_review
      : true,
    lark_app_id: typeof settings.lark_app_id === 'string' ? settings.lark_app_id : '',
    lark_app_secret: typeof settings.lark_app_secret === 'string' ? settings.lark_app_secret : '',
    lark_app_secret_configured: typeof settings.lark_app_secret_configured === 'boolean' ? settings.lark_app_secret_configured : false,
    sql_editor_app_timeout_seconds:
      typeof settings.sql_editor_app_timeout_seconds === 'number' ? settings.sql_editor_app_timeout_seconds : 30,
    sql_editor_mysql_max_execution_time_ms:
      typeof settings.sql_editor_mysql_max_execution_time_ms === 'number' ? settings.sql_editor_mysql_max_execution_time_ms : 25000,
    sql_editor_postgres_statement_timeout_ms:
      typeof settings.sql_editor_postgres_statement_timeout_ms === 'number' ? settings.sql_editor_postgres_statement_timeout_ms : 25000,
    db_metadata_inventory_enabled: typeof settings.db_metadata_inventory_enabled === 'boolean' ? settings.db_metadata_inventory_enabled : true,
    db_metadata_inventory_regions: Array.isArray(settings.db_metadata_inventory_regions) ? settings.db_metadata_inventory_regions : [],
    db_metadata_inventory_engines: Array.isArray(settings.db_metadata_inventory_engines) ? settings.db_metadata_inventory_engines : ['aurora-mysql', 'aurora-postgresql', 'redis'],
    db_metadata_inventory_sync_interval_minutes:
      typeof settings.db_metadata_inventory_sync_interval_minutes === 'number' ? settings.db_metadata_inventory_sync_interval_minutes : 5,
    db_metadata_object_enabled: typeof settings.db_metadata_object_enabled === 'boolean' ? settings.db_metadata_object_enabled : true,
    db_metadata_object_enabled_connection_ids: Array.isArray(settings.db_metadata_object_enabled_connection_ids)
      ? settings.db_metadata_object_enabled_connection_ids
      : [],
    db_metadata_object_sync_interval_minutes:
      typeof settings.db_metadata_object_sync_interval_minutes === 'number' ? settings.db_metadata_object_sync_interval_minutes : 60,
  }
}

export function getSettings() {
  return apiClient.get<PlatformSettings>('/settings').then(normalizeSettings)
}

export function patchSettings(payload: PlatformSettings) {
  return apiClient.patch<PlatformSettings>('/settings', payload).then(normalizeSettings)
}

type SettingsDBConnectionsResponse = {
  connections: Array<Pick<DBConnection, 'id' | 'name' | 'db_type' | 'host' | 'port'>>
}

export function listSettingsDBConnections() {
  return apiClient.get<SettingsDBConnectionsResponse>('/settings/db-connections').then((response) => ({
    ...response,
    connections: Array.isArray(response.connections) ? response.connections : [],
  }))
}
