import { useEffect, useState } from 'react'
import { TableProperties } from 'lucide-react'
import { DBMetadataSectionTabs } from '@/modules/db-metadata/components/DBMetadataSectionTabs'
import { listDBObjectSnapshots } from '@/modules/db-metadata/api'
import { ApiError } from '@/shared/api/client'
import { formatDateTime } from '@/shared/lib/format'
import type { DBObjectSnapshot } from '@/shared/types/dbMetadata'
import { InlineAlert } from '@/shared/ui/InlineAlert'
import { LoadingBlock } from '@/shared/ui/LoadingBlock'
import { Pagination } from '@/shared/ui/Pagination'

const PAGE_SIZE = 20

export function DBMetadataObjectsPage() {
  const [items, setItems] = useState<DBObjectSnapshot[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [offset, setOffset] = useState(0)

  useEffect(() => {
    let active = true

    async function bootstrap() {
      setLoading(true)
      setError('')
      try {
        const response = await listDBObjectSnapshots()
        if (active) {
          setItems(response.items)
        }
      } catch (loadError) {
        if (active) {
          setError(loadError instanceof ApiError ? loadError.message : '讀取資料庫物件快照失敗。')
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
              <TableProperties className="h-5 w-5" />
            </div>
            <div>
              <h2 className="text-[24px] font-bold tracking-[-0.03em] text-ink">DB Metadata / 資料庫物件</h2>
              <p className="mt-2 text-[13px] leading-6 text-muted">
                這裡只顯示 MySQL / PostgreSQL 的 object snapshot。資料來自定時掃描結果，不是頁面即時連 DB。
              </p>
            </div>
          </div>
        </div>
      </section>

      <DBMetadataSectionTabs />

      {error ? <InlineAlert>{error}</InlineAlert> : null}

      <section className="overflow-hidden rounded-xl border border-border bg-panel shadow-soft">
        {loading ? (
          <LoadingBlock message="載入資料庫物件中…" className="min-h-[320px] rounded-none border-0 bg-transparent" />
        ) : items.length === 0 ? (
          <div className="m-4 flex min-h-[240px] items-center justify-center rounded-xl border border-dashed border-border bg-panel-soft text-sm text-muted">
            尚未有任何 object snapshot。
          </div>
        ) : (
          <div className="grid gap-3 p-3">
            <div className="overflow-x-auto">
            <table className="min-w-full border-collapse">
              <thead className="bg-editor-toolbar text-left text-[10px] font-bold uppercase tracking-[0.16em] text-faint">
                <tr>
                  <th className="px-3 py-3">Connection</th>
                  <th className="px-3 py-3">Engine</th>
                  <th className="px-3 py-3">Cluster / Node</th>
                  <th className="px-3 py-3">Database / Schema</th>
                  <th className="px-3 py-3">Table</th>
                  <th className="px-3 py-3">Data Size</th>
                  <th className="px-3 py-3">Index Size</th>
                  <th className="px-3 py-3">Snapshot Time</th>
                </tr>
              </thead>
              <tbody>
                {pagedItems.map((item) => (
                  <tr key={item.id} className="border-t border-border text-sm text-ink hover:bg-slate-50/70">
                    <td className="px-3 py-2.5 align-top text-[12px] font-semibold">{item.connection_name}</td>
                    <td className="px-3 py-2.5 align-top text-[12px]">{item.engine}</td>
                    <td className="px-3 py-2.5 align-top text-[12px]">
                      {(item.cluster_name ?? '-') + ' / ' + (item.node_name ?? '-')}
                    </td>
                    <td className="px-3 py-2.5 align-top text-[12px]">
                      {item.database_name} / {item.schema_name}
                    </td>
                    <td className="px-3 py-2.5 align-top font-mono text-[12px]">{item.table_name}</td>
                    <td className="px-3 py-2.5 align-top text-[12px]">{item.data_size_bytes.toLocaleString()}</td>
                    <td className="px-3 py-2.5 align-top text-[12px]">{item.index_size_bytes.toLocaleString()}</td>
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
