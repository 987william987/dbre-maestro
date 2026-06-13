import { PageTabs } from '@/shared/ui/PageTabs'

const ITEMS = [
  { to: '/db-metadata/inventory', label: 'Inventory' },
  { to: '/db-metadata/objects', label: 'Objects' },
]

export function DBMetadataSectionTabs() {
  return (
    <PageTabs items={ITEMS.map((item) => ({ key: item.to, label: item.label, to: item.to }))} />
  )
}
