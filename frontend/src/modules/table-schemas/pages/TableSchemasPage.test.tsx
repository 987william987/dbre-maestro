import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useAuth } from '@/shared/auth/AuthContext'
import * as api from '@/modules/table-schemas/api'
import { TableSchemasPage } from '@/modules/table-schemas/pages/TableSchemasPage'

vi.mock('@/shared/auth/AuthContext', () => ({ useAuth: vi.fn() }))
vi.mock('@/modules/table-schemas/api', async () => {
  const actual = await vi.importActual<typeof import('@/modules/table-schemas/api')>('@/modules/table-schemas/api')
  return { ...actual, listTableSchemaConnections: vi.fn(), listTableSchemaDatabases: vi.fn(), listTableSchemaTables: vi.fn(), previewTableSchemaExport: vi.fn(), previewTableSchemaSync: vi.fn(), createTableSchemaSyncJob: vi.fn(), downloadTableSchemaExport: vi.fn(), listTableSchemaJobs: vi.fn(), getTableSchemaJob: vi.fn(), cancelTableSchemaJob: vi.fn(), retryTableSchemaJob: vi.fn() }
})

const mockedAuth = vi.mocked(useAuth)
const mockedAPI = vi.mocked(api)

describe('TableSchemasPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockedAuth.mockReturnValue({ user: { permissions: ['table_schemas.read', 'table_schemas.sync'] } } as ReturnType<typeof useAuth>)
    mockedAPI.listTableSchemaConnections.mockResolvedValue([])
    mockedAPI.listTableSchemaJobs.mockResolvedValue({ items: [], total: 0 })
  })

  it('載入 Export 工作區並切換到需要 Target 的 Sync 工作區', async () => {
    render(<TableSchemasPage />)
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Table Schemas' })).toBeInTheDocument())
	await waitFor(() => expect(screen.getByText('No schema sync jobs found.')).toBeInTheDocument())
    expect(screen.getByRole('button', { name: 'Export' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Target' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Sync' }))
    expect(screen.getByRole('heading', { name: 'Target' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Preview' })).toBeDisabled()
  })

  it('沒有 sync 權限時仍可開啟頁面，但 Sync preview 保持停用', async () => {
    mockedAuth.mockReturnValue({ user: { permissions: ['table_schemas.read'] } } as ReturnType<typeof useAuth>)
    render(<TableSchemasPage />)
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Table Schemas' })).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: 'Sync' }))
    expect(screen.getByRole('button', { name: 'Preview' })).toBeDisabled()
  })

  it('呈現 partial failure detail 與逐表耗時', async () => {
	const job = schemaJob({ id: 8, status: 'failed', created_count: 1, failed_count: 1, table_count: 2, error_code: 'create_table_failed' })
	mockedAPI.listTableSchemaJobs.mockResolvedValue({ items: [job], total: 1 })
	mockedAPI.getTableSchemaJob.mockResolvedValue({ job, items: [{ id: 1, job_id: 8, table_name: 'orders', dependency_order: 1, status: 'failed', duration_ms: 12, error_code: 'create_table_failed' }] })
	render(<TableSchemasPage />)
	await screen.findByText('#8')
	fireEvent.click(screen.getByRole('button', { name: 'View job 8' }))
	expect(await screen.findByText('orders')).toBeInTheDocument()
	expect(screen.getByText('12 ms')).toBeInTheDocument()
  })

  it('確認後送出 cancel，retry drift 則顯示後端拒絕原因', async () => {
	const queued = schemaJob({ id: 9, status: 'queued' })
	mockedAPI.listTableSchemaJobs.mockResolvedValue({ items: [queued], total: 1 })
	mockedAPI.cancelTableSchemaJob.mockResolvedValue({ cancel_requested: true })
	render(<TableSchemasPage />)
	await screen.findByText('#9')
	fireEvent.click(screen.getByRole('button', { name: 'Cancel job 9' }))
	fireEvent.click(screen.getByRole('button', { name: 'Request Cancel' }))
	await waitFor(() => expect(mockedAPI.cancelTableSchemaJob).toHaveBeenCalledWith(9))

	const failed = schemaJob({ id: 10, status: 'failed' })
	mockedAPI.listTableSchemaJobs.mockResolvedValue({ items: [failed], total: 1 })
	await waitFor(() => expect(screen.getByText(/Cancellation requested/)).toBeInTheDocument())
	fireEvent.click(screen.getByRole('button', { name: 'Refresh sync jobs' }))
	await screen.findByText('#10')
	mockedAPI.retryTableSchemaJob.mockRejectedValue(new Error('a previously created target table changed or is missing'))
	fireEvent.click(screen.getByRole('button', { name: 'Retry job 10' }))
	fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
	expect(await screen.findByText('a previously created target table changed or is missing')).toBeInTheDocument()
  })

  it('快速切換 job detail 時忽略較晚回來的舊 response', async () => {
	const first = schemaJob({ id: 11 }), second = schemaJob({ id: 12 })
	mockedAPI.listTableSchemaJobs.mockResolvedValue({ items: [first, second], total: 2 })
	const oldResponse = deferred<Awaited<ReturnType<typeof api.getTableSchemaJob>>>()
	const newResponse = deferred<Awaited<ReturnType<typeof api.getTableSchemaJob>>>()
	mockedAPI.getTableSchemaJob.mockImplementation((id) => id === 11 ? oldResponse.promise : newResponse.promise)
	render(<TableSchemasPage />)
	await screen.findByText('#11')
	fireEvent.click(screen.getByRole('button', { name: 'View job 11' }))
	fireEvent.click(screen.getByRole('button', { name: 'View job 12' }))
	newResponse.resolve({ job: second, items: [{ id: 12, job_id: 12, table_name: 'new_table', dependency_order: 0, status: 'created' }] })
	expect(await screen.findByText('new_table')).toBeInTheDocument()
	oldResponse.resolve({ job: first, items: [{ id: 11, job_id: 11, table_name: 'stale_table', dependency_order: 0, status: 'created' }] })
	await waitFor(() => expect(screen.queryByText('stale_table')).not.toBeInTheDocument())
  })

  it('完成 source 選取後顯示 deterministic export preview', async () => {
	prepareMetadata()
	mockedAPI.previewTableSchemaExport.mockResolvedValue({ database: 'source', tables: [{ name: 'orders', source: { engine: 'InnoDB' }, output: { engine: 'InnoDB' } }], order: ['orders'], external_dependencies: [], warnings: [], script: 'CREATE TABLE `orders` (`id` bigint);' })
	render(<TableSchemasPage />)
	await selectSource()
	fireEvent.click(screen.getByRole('button', { name: 'Preview' }))
	expect(await screen.findByText('CREATE TABLE `orders` (`id` bigint);')).toBeInTheDocument()
	expect(screen.getByText('Order: orders')).toBeInTheDocument()
  })

  it('Sync preview 發現 target conflict 時顯示穩定錯誤', async () => {
	prepareMetadata()
	mockedAPI.previewTableSchemaSync.mockRejectedValue(new Error('one or more target tables already exist'))
	render(<TableSchemasPage />)
	await selectSource()
	fireEvent.click(screen.getByRole('button', { name: 'Sync' }))
	fireEvent.click(screen.getByRole('button', { name: 'Target connection' }))
	fireEvent.click(screen.getByRole('option', { name: 'Primary' }))
	await waitFor(() => expect(mockedAPI.listTableSchemaDatabases).toHaveBeenCalledTimes(2))
	fireEvent.click(screen.getByRole('button', { name: 'Target database' }))
	fireEvent.click(screen.getByRole('option', { name: 'target' }))
	fireEvent.click(screen.getByRole('button', { name: 'Preview' }))
	expect(await screen.findByText('one or more target tables already exist')).toBeInTheDocument()
  })

  it('Export preview 發現 foreign key cycle 時不顯示過期 preview', async () => {
	prepareMetadata()
	mockedAPI.previewTableSchemaExport.mockRejectedValue(new Error('selected tables contain a foreign key cycle'))
	render(<TableSchemasPage />)
	await selectSource()
	fireEvent.click(screen.getByRole('button', { name: 'Preview' }))
	expect(await screen.findByText('selected tables contain a foreign key cycle')).toBeInTheDocument()
	expect(screen.queryByRole('heading', { name: 'Preview' })).not.toBeInTheDocument()
  })
})

function prepareMetadata() {
  mockedAPI.listTableSchemaConnections.mockResolvedValue([{ id: 1, name: 'Primary' }])
  mockedAPI.listTableSchemaDatabases.mockResolvedValue(['source', 'target'])
  mockedAPI.listTableSchemaTables.mockResolvedValue({ items: [{ name: 'orders', options: { engine: 'InnoDB' } }], dependencies: [] })
}

async function selectSource() {
  await screen.findByRole('heading', { name: 'Table Schemas' })
  fireEvent.click(screen.getByRole('button', { name: 'Source connection' }))
  fireEvent.click(screen.getByRole('option', { name: 'Primary' }))
  await waitFor(() => expect(mockedAPI.listTableSchemaDatabases).toHaveBeenCalled())
  fireEvent.click(screen.getByRole('button', { name: 'Source database' }))
  fireEvent.click(screen.getByRole('option', { name: 'source' }))
  await waitFor(() => expect(mockedAPI.listTableSchemaTables).toHaveBeenCalled())
  fireEvent.click(screen.getByRole('button', { name: 'Source tables' }))
  fireEvent.click(screen.getByRole('option', { name: 'orders' }))
}

function schemaJob(overrides: Partial<api.TableSchemaJob> = {}): api.TableSchemaJob {
  return { id: 1, requested_by: 7, source_connection_id: 1, source_database: 'source', target_connection_id: 2, target_database: 'target', transformation_config: { reset_auto_increment: false, engine: '', charset: '', collation: '', row_format: '' }, status: 'completed', table_count: 1, created_count: 1, failed_count: 0, not_started_count: 0, created_at: '2026-10-03T00:00:00Z', updated_at: '2026-10-03T00:00:00Z', ...overrides }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((next) => { resolve = next })
  return { promise, resolve }
}
