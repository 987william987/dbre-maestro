import { useEffect, useState } from 'react'
import { DatabaseZap } from 'lucide-react'
import { DBMetadataSectionTabs } from '@/modules/db-metadata/components/DBMetadataSectionTabs'
import { listInventorySnapshots } from '@/modules/db-metadata/api'
import { ApiError } from '@/shared/api/client'
import { formatDateTime } from '@/shared/lib/format'
import type { InventorySnapshot } from '@/shared/types/dbMetadata'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { Pagination } from '@/shared/ui/Pagination'

const PAGE_SIZE = 10

export function DBMetadataInventoryPage() {
  const [items, setItems] = useState<InventorySnapshot[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [offset, setOffset] = useState(0)

  useEffect(() => {
    let active = true

    async function bootstrap() {
      setLoading(true)
      setError('')
      try {
        const response = await listInventorySnapshots()
        if (active) {
          setItems(response.items)
        }
      } catch (loadError) {
        if (active) {
          setError(loadError instanceof ApiError ? loadError.message : '讀取實例總覽失敗。')
        }
      } finally {
        if (active) {
          setLoading(false)
        }
      }
    }

    void bootstrap()
    return () => {
      active = false
    }
  }, [])

  const pagedItems = items.slice(offset, offset + PAGE_SIZE)

  return (
    <div className="flex h-full flex-col gap-3 p-3 sm:p-4">
      <section className="rounded-xl border border-border bg-panel-soft shadow-soft">
        <div className="px-4 py-4 sm:px-5">
          <div className="flex items-start gap-3">
            <div className="inline-flex h-11 w-11 items-center justify-center rounded-xl bg-white text-ink shadow-soft">
              <DatabaseZap className="h-5 w-5" />
            </div>
            <div>
              <h2 className="text-[24px] font-bold tracking-[-0.03em] text-ink">DB Metadata / 實例總覽</h2>
              <p className="mt-2 text-[13px] leading-6 text-muted">
                這裡顯示 AWS inventory snapshot，不做即時監控；`Mapping` 只根據 endpoint 與 `DB Connection host` 的 exact match。
              </p>
            </div>
          </div>
        </div>
      </section>

      <DBMetadataSectionTabs />

      {error ? <InlineAlert>{error}</InlineAlert> : null}

      <section className="overflow-hidden rounded-xl border border-border bg-panel shadow-soft">
        {loading ? (
          <LoadingBlock message="載入實例總覽中…" className="min-h-[320px] rounded-none border-0 bg-transparent" />
        ) : items.length === 0 ? (
          <div className="m-4 flex min-h-[240px] items-center justify-center rounded-xl border border-dashed border-border bg-panel-soft text-sm text-muted">
            尚未有任何 inventory snapshot。
          </div>
        ) : (
          <div className="grid gap-3 p-3">
            <div className="overflow-x-auto">
            <table className="min-w-full border-collapse">
              <thead className="bg-editor-toolbar text-left text-[10px] font-bold uppercase tracking-[0.16em] text-faint">
                <tr>
                  <th className="px-3 py-3">Identifier</th>
                  <th className="px-3 py-3">Engine</th>
                  <th className="px-3 py-3">Version</th>
                  <th className="px-3 py-3">Region / AZ</th>
                  <th className="px-3 py-3">Role</th>
                  <th className="px-3 py-3">Size</th>
                  <th className="px-3 py-3">Mapping</th>
                  <th className="px-3 py-3">Last Synced</th>
                </tr>
              </thead>
              <tbody>
                {pagedItems.map((item) => (
                  <tr key={item.id} className="border-t border-border text-sm text-ink hover:bg-slate-50/70">
                    <td className="px-3 py-2.5 align-top">
                      <p className="font-semibold">{item.db_identifier}</p>
                      <p className="mt-1 break-all font-mono text-[11px] text-muted">{item.instance_endpoint ?? item.cluster_endpoint ?? '-'}</p>
                    </td>
                    <td className="px-3 py-2.5 align-top text-[12px]">{item.engine}</td>
                    <td className="px-3 py-2.5 align-top text-[12px]">{item.engine_version ?? '-'}</td>
                    <td className="px-3 py-2.5 align-top text-[12px]">
                      {item.region}
                      {item.az ? ` / ${item.az}` : ''}
                    </td>
                    <td className="px-3 py-2.5 align-top text-[12px]">{item.role ?? '-'}</td>
                    <td className="px-3 py-2.5 align-top text-[12px]">{item.instance_class ?? '-'}</td>
                    <td className="px-3 py-2.5 align-top text-[12px]">
                      <p className="font-semibold">{item.mapping_status}</p>
                      <p className="mt-1 text-muted">{item.mapping_connections?.join(', ') || '-'}</p>
                    </td>
                    <td className="px-3 py-2.5 align-top whitespace-nowrap text-[12px] text-muted">{formatDateTime(item.snapshot_at)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
            <Pagination
              offset={offset}
              pageSize={PAGE_SIZE}
              count={pagedItems.length}
              total={items.length}
              onChange={setOffset}
            />
          </div>
        )}
      </section>
    </div>
  )
}
