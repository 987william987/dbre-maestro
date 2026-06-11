import { apiClient } from '@/shared/api/client'
import type { PlatformSettings } from '@/shared/types/settings'

function normalizeSettings(settings: PlatformSettings): PlatformSettings {
  return {
    sensitive_export_reviewer_user_ids: Array.isArray(settings.sensitive_export_reviewer_user_ids)
      ? settings.sensitive_export_reviewer_user_ids
      : [],
    sensitive_query_access_reviewer_user_ids: Array.isArray(settings.sensitive_query_access_reviewer_user_ids)
      ? settings.sensitive_query_access_reviewer_user_ids
      : [],
  }
}

export function getSettings() {
  return apiClient.get<PlatformSettings>('/settings').then(normalizeSettings)
}

export function patchSettings(payload: PlatformSettings) {
  return apiClient.patch<PlatformSettings>('/settings', payload).then(normalizeSettings)
}
