import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import * as binlogApi from '@/modules/binlog-export/api'
import { BinlogExportPage } from './BinlogExportPage'
import { useAuth } from '@/shared/auth/AuthContext'

vi.mock('@/modules/binlog-export/api', () => ({
  listBinlogExportConnections: vi.fn(),
  listBinlogExportJobs: vi.fn(),
  listBinlogs: vi.fn(),
  listBinlogDatabases: vi.fn(),
  listBinlogTables: vi.fn(),
  createBinlogExport: vi.fn(),
  cancelBinlogExport: vi.fn(),
  retryBinlogExport: vi.fn(),
  downloadBinlogArtifact: vi.fn(),
  previewBinlogArtifact: vi.fn(),
  probeBinlogTimestamps: vi.fn(),
}))
vi.mock('@/shared/auth/AuthContext', () => ({ useAuth: vi.fn() }))

const mockedUseAuth = vi.mocked(useAuth)
const mockedListConnections = vi.mocked(binlogApi.listBinlogExportConnections)
const mockedListJobs = vi.mocked(binlogApi.listBinlogExportJobs)
const mockedListBinlogs = vi.mocked(binlogApi.listBinlogs)
const mockedListDatabases = vi.mocked(binlogApi.listBinlogDatabases)
const mockedListTables = vi.mocked(binlogApi.listBinlogTables)
const mockedCreate = vi.mocked(binlogApi.createBinlogExport)
const mockedCancel = vi.mocked(binlogApi.cancelBinlogExport)
const mockedRetry = vi.mocked(binlogApi.retryBinlogExport)
const mockedDownload = vi.mocked(binlogApi.downloadBinlogArtifact)
const mockedPreview = vi.mocked(binlogApi.previewBinlogArtifact)
const mockedProbeTimes = vi.mocked(binlogApi.probeBinlogTimestamps)

const baseJob: binlogApi.BinlogExportJob = {
  id: 41,
  requested_by: 7,
  source_connection_id: 3,
  range_mode: 'time',
  timezone: 'Asia/Taipei',
  requested_start_time: '2026-10-01T01:00:00Z',
  requested_end_time: '2026-10-01T01:05:00Z',
  source_database_name: 'billing',
  source_tables: ['orders'],
  dml_types: ['insert', 'update', 'delete'],
  acknowledged_unfiltered: false,
  status: 'succeeded',
  phase: 'completed',
  created_at: '2026-10-01T01:06:00Z',
  updated_at: '2026-10-01T01:06:00Z',
}

function setPermissions(permissions: string[]) {
  mockedUseAuth.mockReturnValue({ user: { permissions } } as ReturnType<typeof useAuth>)
}

function selectOption(label: string, option: string) {
  fireEvent.click(screen.getByRole('button', { name: label }))
  fireEvent.click(screen.getByRole('option', { name: option }))
}

async function selectConnection() {
  expect(await screen.findByText('New Export')).toBeInTheDocument()
  selectOption('DB Connection', 'Primary MySQL')
  await waitFor(() => expect(mockedListBinlogs).toHaveBeenCalledWith(3))
}

describe('BinlogExportPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setPermissions(['binlog_exports.read', 'binlog_exports.execute'])
    mockedListConnections.mockResolvedValue([{ id: 3, name: 'Primary MySQL' }])
    mockedListJobs.mockResolvedValue({ items: [baseJob], total: 1 })
    mockedListBinlogs.mockResolvedValue({
      items: [{ name: 'mysql-bin.000010', size_bytes: 4096, active: true }],
      time_metadata_available: false,
    })
    mockedListDatabases.mockResolvedValue(['billing'])
    mockedListTables.mockResolvedValue(['orders', 'payments'])
    mockedCreate.mockResolvedValue({ id: 42, status: 'queued' })
    mockedCancel.mockResolvedValue({ ok: true })
    mockedRetry.mockResolvedValue({ id: 43, status: 'queued' })
    mockedDownload.mockResolvedValue(undefined)
	mockedPreview.mockResolvedValue({ sql: 'INSERT INTO orders VALUES (1);', truncated: false, expires_at: '2099-01-01T00:00:00Z' })
    mockedProbeTimes.mockResolvedValue([{ file: 'mysql-bin.000010', start_time: '2026-10-01T00:00:00Z' }])
  })

  it('載入 connection、binlog inventory 與既有 jobs', async () => {
    render(<BinlogExportPage />)
    await selectConnection()

    expect(mockedListJobs).toHaveBeenCalledWith(20, 0)
    expect(await screen.findByText('mysql-bin.000010')).toBeInTheDocument()
    expect(screen.getByText('#41')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Download forward SQL for job 41' })).toBeInTheDocument()
  })

  it('不重複顯示 AppShell 已提供的頁面標題', async () => {
    render(<BinlogExportPage />)
    await screen.findByText('New Export')

    expect(screen.queryByRole('heading', { name: 'MySQL Binlog Export' })).not.toBeInTheDocument()
  })

  it('初始資料載入失敗時顯示錯誤', async () => {
    mockedListConnections.mockRejectedValue(new Error('offline'))
    render(<BinlogExportPage />)
    expect(await screen.findByText('Failed to load MySQL Binlog Export.')).toBeInTheDocument()
  })

	it('只在使用者要求時探測並顯示 binlog 時間', async () => {
		render(<BinlogExportPage />)
		await selectConnection()
		expect(mockedProbeTimes).not.toHaveBeenCalled()
		fireEvent.click(screen.getByRole('button', { name: 'Probe times' }))
		await waitFor(() => expect(mockedProbeTimes).toHaveBeenCalledWith(3, 'mysql-bin.000010'))
		await waitFor(() => expect(screen.getByText(/08:00:00/)).toBeInTheDocument())
		expect(screen.getByText('Active')).toBeInTheDocument()
	})

	it('timestamp probe 失敗時保留 inventory 並顯示錯誤', async () => {
		mockedProbeTimes.mockRejectedValue(new Error('probe failed'))
		render(<BinlogExportPage />)
		await selectConnection()
		fireEvent.click(screen.getByRole('button', { name: 'Probe times' }))
		expect(await screen.findByText('Failed to probe 1 binlog file.')).toBeInTheDocument()
		expect(screen.getByText('mysql-bin.000010')).toBeInTheDocument()
	})

	it('逐檔探測並以下一檔開始時間作為上一檔結束邊界', async () => {
		mockedListBinlogs.mockResolvedValue({
			items: [
				{ name: 'mysql-bin.000009', size_bytes: 2048, active: false },
				{ name: 'mysql-bin.000010', size_bytes: 4096, active: true },
			],
			time_metadata_available: false,
		})
		mockedProbeTimes
			.mockResolvedValueOnce([{ file: 'mysql-bin.000009', start_time: '2026-10-01T00:00:00Z' }])
			.mockResolvedValueOnce([{ file: 'mysql-bin.000010', start_time: '2026-10-01T00:05:00Z' }])
		render(<BinlogExportPage />)
		await selectConnection()
		fireEvent.click(screen.getByRole('button', { name: 'Probe times' }))
		await waitFor(() => expect(mockedProbeTimes).toHaveBeenNthCalledWith(2, 3, 'mysql-bin.000010'))
		expect(screen.getAllByText(/08:05:00/)).toHaveLength(2)
		expect(screen.getByText('Active')).toBeInTheDocument()
	})

  it('read-only 使用者可查看，但不能建立或取消 job', async () => {
    setPermissions(['binlog_exports.read'])
    mockedListJobs.mockResolvedValue({ items: [{ ...baseJob, status: 'running', phase: 'reading_binlog' }], total: 1 })
    render(<BinlogExportPage />)

    expect(await screen.findByRole('button', { name: 'Queue Export' })).toBeDisabled()
    expect(screen.queryByRole('button', { name: 'Cancel job 41' })).not.toBeInTheDocument()
  })

  it('time mode 會送出 timezone、資料篩選與 DML filters', async () => {
    render(<BinlogExportPage />)
    await selectConnection()
    expect(screen.getByRole('button', { name: 'Timezone' })).toHaveTextContent('UTC (UTC+00:00)')
    fireEvent.change(screen.getByLabelText('Start time'), { target: { value: '2026-10-01T09:00' } })
    fireEvent.change(screen.getByLabelText('End time'), { target: { value: '2026-10-01T09:05' } })
    fireEvent.click(screen.getByRole('button', { name: 'Timezone' }))
    fireEvent.change(screen.getByLabelText('Timezone search'), { target: { value: 'Asia/Taipei' } })
    fireEvent.click(screen.getByRole('option', { name: /Asia\/Taipei/ }))
    selectOption('Database', 'billing')
    await waitFor(() => expect(mockedListTables).toHaveBeenCalledWith(3, 'billing'))
    fireEvent.click(screen.getByRole('button', { name: 'Tables' }))
    fireEvent.click(screen.getByRole('option', { name: 'orders' }))
    fireEvent.click(screen.getByRole('button', { name: 'Queue Export' }))

    await waitFor(() => expect(mockedCreate).toHaveBeenCalledWith({
      source_connection_id: 3,
      range_mode: 'time',
      timezone: 'Asia/Taipei',
      start_time: '2026-10-01T01:00:00.000Z',
      end_time: '2026-10-01T01:05:00.000Z',
      database: 'billing',
      tables: ['orders'],
      dml_types: ['insert', 'update', 'delete'],
      acknowledge_unfiltered: false,
    }))
    expect(await screen.findByText('Binlog export #42 queued.')).toBeInTheDocument()
  })

  it('預設 UTC 時間欄位與送出的 ISO instant 維持相同 wall time', async () => {
    render(<BinlogExportPage />)
    await selectConnection()
    fireEvent.change(screen.getByLabelText('Start time'), { target: { value: '2026-10-01T01:00' } })
    fireEvent.change(screen.getByLabelText('End time'), { target: { value: '2026-10-01T01:05' } })
    selectOption('Database', 'billing')
    fireEvent.click(screen.getByRole('button', { name: 'Queue Export' }))

    await waitFor(() => expect(mockedCreate).toHaveBeenCalledWith(expect.objectContaining({
      timezone: 'UTC',
      start_time: '2026-10-01T01:00:00.000Z',
      end_time: '2026-10-01T01:05:00.000Z',
    })))
  })

  it('export jobs 使用後端 total 並以 limit/offset 翻頁', async () => {
    mockedListJobs
      .mockResolvedValueOnce({ items: [baseJob], total: 41 })
      .mockResolvedValueOnce({ items: [{ ...baseJob, id: 61 }], total: 41 })
    render(<BinlogExportPage />)

    expect(await screen.findByText('41 jobs')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Next' }))
    await waitFor(() => expect(mockedListJobs).toHaveBeenLastCalledWith(20, 20))
    expect(await screen.findByText('#61')).toBeInTheDocument()
    expect(screen.getByText('Showing 21–21 of 41')).toBeInTheDocument()
  })

  it('從 Export Jobs 區塊刷新目前頁面', async () => {
    render(<BinlogExportPage />)
    const refreshButton = await screen.findByRole('button', { name: 'Refresh export jobs' })
    expect(refreshButton.closest('section')).toContainElement(screen.getByRole('heading', { name: 'Export Jobs' }))

    fireEvent.click(refreshButton)
    await waitFor(() => expect(mockedListJobs).toHaveBeenLastCalledWith(20, 0))
  })

  it('table selector 支援搜尋與多選，選取結果完整送入 payload', async () => {
    render(<BinlogExportPage />)
    await selectConnection()
    selectOption('Database', 'billing')
    await waitFor(() => expect(mockedListTables).toHaveBeenCalledWith(3, 'billing'))
    fireEvent.click(screen.getByRole('button', { name: 'Tables' }))
    fireEvent.change(screen.getByLabelText('Tables search'), { target: { value: 'order' } })
    expect(screen.getByRole('option', { name: 'orders' })).toBeInTheDocument()
    expect(screen.queryByRole('option', { name: 'payments' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('option', { name: 'orders' }))
    fireEvent.change(screen.getByLabelText('Tables search'), { target: { value: '' } })
    fireEvent.click(screen.getByRole('option', { name: 'payments' }))
    fireEvent.click(screen.getByRole('button', { name: 'Queue Export' }))
    await waitFor(() => expect(mockedCreate).toHaveBeenCalledWith(expect.objectContaining({ tables: ['orders', 'payments'] })))
  })

  it('未指定 database 或 table 時必須先確認風險', async () => {
    render(<BinlogExportPage />)
    await selectConnection()
    expect(screen.getByRole('button', { name: 'Queue Export' })).toBeDisabled()
    fireEvent.click(screen.getByLabelText('Acknowledge unfiltered export'))
    expect(screen.getByRole('button', { name: 'Queue Export' })).toBeEnabled()
  })

  it('position mode 送出選定的 binlog positions', async () => {
    render(<BinlogExportPage />)
    await selectConnection()
    fireEvent.click(screen.getByRole('button', { name: 'position' }))
    fireEvent.change(screen.getByLabelText('Start position'), { target: { value: '120' } })
    fireEvent.change(screen.getByLabelText('End position'), { target: { value: '4000' } })
    fireEvent.click(screen.getByLabelText('Acknowledge unfiltered export'))
    fireEvent.click(screen.getByRole('button', { name: 'Queue Export' }))

    await waitFor(() => expect(mockedCreate).toHaveBeenCalledWith(expect.objectContaining({
      range_mode: 'position',
      start_file: 'mysql-bin.000010',
      start_pos: 120,
      end_file: 'mysql-bin.000010',
      end_pos: 4000,
    })))
  })

  it('建立失敗時保留表單並顯示錯誤', async () => {
    mockedCreate.mockRejectedValue(new Error('failed'))
    render(<BinlogExportPage />)
    await selectConnection()
    fireEvent.click(screen.getByLabelText('Acknowledge unfiltered export'))
    fireEvent.click(screen.getByRole('button', { name: 'Queue Export' }))

    expect(await screen.findByText('Failed to create binlog export.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'DB Connection' })).toHaveTextContent('Primary MySQL')
  })

  it('可取消、重試與下載 jobs', async () => {
    mockedListJobs.mockResolvedValue({
      items: [
        { ...baseJob, id: 51, status: 'running', phase: 'reading_binlog' },
        { ...baseJob, id: 52, status: 'failed', error_message: 'my2sql failed' },
        baseJob,
      ],
      total: 3,
    })
    render(<BinlogExportPage />)
    expect(await screen.findByRole('button', { name: 'Cancel job 51' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Cancel job 51' }))
    await waitFor(() => expect(mockedCancel).toHaveBeenCalledWith(51))
    fireEvent.click(screen.getByRole('button', { name: 'Retry job 52' }))
    await waitFor(() => expect(mockedRetry).toHaveBeenCalledWith(52))
    fireEvent.click(screen.getByRole('button', { name: 'Download rollback SQL for job 41' }))
    await waitFor(() => expect(mockedDownload).toHaveBeenCalledWith(41, 'rollback_sql'))
  })

	it('可預覽 artifact 並關閉 preview', async () => {
		render(<BinlogExportPage />)
		fireEvent.click(await screen.findByRole('button', { name: 'Preview forward SQL for job 41' }))
		await waitFor(() => expect(mockedPreview).toHaveBeenCalledWith(41, 'forward_sql'))
		expect(await screen.findByRole('dialog', { name: '#41 Forward SQL' })).toHaveTextContent('INSERT INTO orders VALUES (1);')
		fireEvent.click(screen.getByRole('button', { name: 'Close preview' }))
		expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
	})

	it('truncated preview 明確提示必須下載完整 artifact', async () => {
		mockedPreview.mockResolvedValue({ sql: 'INSERT;', truncated: true, expires_at: '2099-01-01T00:00:00Z' })
		render(<BinlogExportPage />)
		fireEvent.click(await screen.findByRole('button', { name: 'Preview forward SQL for job 41' }))
		expect(await screen.findByText(/first 64 KiB/)).toBeInTheDocument()
	})

	it('artifact action 失敗時顯示錯誤且解除按鈕鎖定', async () => {
		mockedPreview.mockRejectedValue(new Error('preview failed'))
		render(<BinlogExportPage />)
		const button = await screen.findByRole('button', { name: 'Preview forward SQL for job 41' })
		fireEvent.click(button)
		expect(await screen.findByText('Failed to preview artifact.')).toBeInTheDocument()
		await waitFor(() => expect(button).toBeEnabled())
	})

	it('cancel 失敗時維持原 job 並顯示錯誤', async () => {
		mockedListJobs.mockResolvedValue({ items: [{ ...baseJob, status: 'running' }], total: 1 })
		mockedCancel.mockRejectedValue(new Error('conflict'))
		render(<BinlogExportPage />)
		const button = await screen.findByRole('button', { name: 'Cancel job 41' })
		fireEvent.click(button)
		expect(await screen.findByText('Failed to cancel binlog export.')).toBeInTheDocument()
		expect(screen.getByText('#41')).toBeInTheDocument()
	})

	it('retry 失敗時維持原 job 並顯示錯誤', async () => {
		mockedListJobs.mockResolvedValue({ items: [{ ...baseJob, status: 'failed' }], total: 1 })
		mockedRetry.mockRejectedValue(new Error('retry failed'))
		render(<BinlogExportPage />)
		fireEvent.click(await screen.findByRole('button', { name: 'Retry job 41' }))
		expect(await screen.findByText('Failed to retry binlog export.')).toBeInTheDocument()
		expect(screen.getByText('#41')).toBeInTheDocument()
	})

	it('download 失敗時顯示錯誤且解除按鈕鎖定', async () => {
		mockedDownload.mockRejectedValue(new Error('download failed'))
		render(<BinlogExportPage />)
		const button = await screen.findByRole('button', { name: 'Download forward SQL for job 41' })
		fireEvent.click(button)
		expect(await screen.findByText('Failed to download artifact.')).toBeInTheDocument()
		await waitFor(() => expect(button).toBeEnabled())
	})

	it('artifact 到期後顯示 expired 且不提供 preview 或 download', async () => {
		mockedListJobs.mockResolvedValue({ items: [{ ...baseJob, artifact_expires_at: '2020-01-01T00:00:00Z' }], total: 1 })
		render(<BinlogExportPage />)
		expect(await screen.findByText('Artifacts expired')).toBeInTheDocument()
		expect(screen.queryByRole('button', { name: 'Preview forward SQL for job 41' })).not.toBeInTheDocument()
		expect(screen.queryByRole('button', { name: 'Download forward SQL for job 41' })).not.toBeInTheDocument()
	})

	it('active job 會 polling，進入 terminal state 後停止', async () => {
		vi.useFakeTimers({ shouldAdvanceTime: true })
		mockedListJobs
			.mockResolvedValueOnce({ items: [{ ...baseJob, status: 'running' }], total: 1 })
			.mockResolvedValueOnce({ items: [{ ...baseJob, status: 'succeeded' }], total: 1 })
		render(<BinlogExportPage />)
		await screen.findByRole('button', { name: 'Cancel job 41' })
		await act(async () => { await vi.advanceTimersByTimeAsync(2000) })
		await waitFor(() => expect(mockedListJobs).toHaveBeenCalledTimes(2))
		expect(mockedListJobs).toHaveBeenLastCalledWith(20, 0)
		await act(async () => { await vi.advanceTimersByTimeAsync(4000) })
		expect(mockedListJobs).toHaveBeenCalledTimes(2)
		vi.useRealTimers()
	})

	it('頁面 unmount 後停止 active job polling', async () => {
		vi.useFakeTimers({ shouldAdvanceTime: true })
		mockedListJobs.mockResolvedValue({ items: [{ ...baseJob, status: 'running' }], total: 1 })
		const view = render(<BinlogExportPage />)
		await screen.findByRole('button', { name: 'Cancel job 41' })
		view.unmount()
		await act(async () => { await vi.advanceTimersByTimeAsync(4000) })
		expect(mockedListJobs).toHaveBeenCalledTimes(1)
		vi.useRealTimers()
	})

	it('interrupted job 顯示中斷狀態並允許重試', async () => {
		mockedListJobs.mockResolvedValue({ items: [{ ...baseJob, status: 'interrupted', error_message: 'service restarted' }], total: 1 })
		render(<BinlogExportPage />)
		expect(await screen.findByText('interrupted')).toBeInTheDocument()
		expect(screen.getByText('service restarted')).toBeInTheDocument()
		expect(screen.getByRole('button', { name: 'Retry job 41' })).toBeInTheDocument()
	})
})
