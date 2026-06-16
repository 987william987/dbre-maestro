import { apiClient } from '@/shared/api/client'
import type { DBObjectSnapshot, InventorySnapshot } from '@/shared/types/dbMetadata'

type InventoryResponse = {
  items: InventorySnapshot[]
  total: number
}

type ObjectResponse = {
  items: DBObjectSnapshot[]
  total: number
}

export function listInventorySnapshots() {
  return apiClient.get<InventoryResponse>('/db-metadata/inventory').then((response) => ({
    ...response,
    items: Array.isArray(response.items) ? response.items : [],
  }))
}

export function listDBObjectSnapshots() {
  return apiClient.get<ObjectResponse>('/db-metadata/objects').then((response) => ({
    ...response,
    items: Array.isArray(response.items) ? response.items : [],
  }))
}
