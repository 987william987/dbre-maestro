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
