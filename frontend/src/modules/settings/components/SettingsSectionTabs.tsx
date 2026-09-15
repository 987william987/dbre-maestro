import { PageTabs } from '@/shared/ui/PageTabs'

export const SETTINGS_SECTIONS = [
  { key: 'workflow', label: 'Workflow', to: '/settings/workflow' },
  { key: 'scans', label: 'Scans', to: '/settings/scans' },
  { key: 'query-execution', label: 'Query & Execution', to: '/settings/query-execution' },
  { key: 'integrations', label: 'Integrations', to: '/settings/integrations' },
] as const

export type SettingsSection = (typeof SETTINGS_SECTIONS)[number]['key']

export function SettingsSectionTabs() {
  return <PageTabs items={SETTINGS_SECTIONS.map((item) => ({ key: item.key, label: item.label, to: item.to }))} />
}
