import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import * as api from '@/modules/session-management/api'
import { useAuth } from '@/shared/auth/AuthContext'
import { SessionManagementPage } from './SessionManagementPage'

vi.mock('@/modules/session-management/api', () => ({
  listSessionConnections: vi.fn(), listAWSClusters: vi.fn(), getAWSTopology: vi.fn(), listSessions: vi.fn(), cancelSession: vi.fn(), terminateSession: vi.fn(),
  previewPrefix: vi.fn(), cancelPrefix: vi.fn(), listLoopJobs: vi.fn(), createLoopJob: vi.fn(), stopLoopJob: vi.fn(),
}))
vi.mock('@/shared/auth/AuthContext', () => ({ useAuth: vi.fn() }))

const listConnections = vi.mocked(api.listSessionConnections)
const listClusters = vi.mocked(api.listAWSClusters)
const getTopology = vi.mocked(api.getAWSTopology)
const readSessions = vi.mocked(api.listSessions)
const cancel = vi.mocked(api.cancelSession)
const terminate = vi.mocked(api.terminateSession)
const preview = vi.mocked(api.previewPrefix)
const cancelByPrefix = vi.mocked(api.cancelPrefix)
const loopJobs = vi.mocked(api.listLoopJobs)
const createLoop = vi.mocked(api.createLoopJob)
const stopLoop = vi.mocked(api.stopLoopJob)
const auth = vi.mocked(useAuth)
const cluster: api.SessionCluster = { id: 'orders', engine: 'aurora-mysql', region: 'ap-northeast-1', nodes: [{ id: 'orders-1', role: 'writer', host: 'orders-1.aws', port: 3306 }] }

function choose(label: string, option: string) {
  fireEvent.click(screen.getByRole('button', { name: label }))
  fireEvent.click(screen.getByRole('option', { name: option }))
}

describe('SessionManagementPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    auth.mockReturnValue({ user: { permissions: ['db_sessions.read', 'db_sessions.kill'] } } as ReturnType<typeof useAuth>)
    listConnections.mockResolvedValue([{ id: 7, name: 'Orders', db_type: 'mysql', operations_configured: true }])
    listClusters.mockResolvedValue([cluster])
    getTopology.mockResolvedValue(cluster)
    readSessions.mockResolvedValue({ items: [{ id: '42', user: 'app', database: 'orders', client: '10.0.0.8', state: 'Query', duration_seconds: 75, query: 'SELECT * FROM orders', query_hash: 'abc123', backend_start: '2026-10-02 12:00:00+00', protected: true, protected_reason: 'tool-owned session' }], truncated: false })
    cancel.mockResolvedValue({ ok: true })
    terminate.mockResolvedValue({ ok: true })
    loopJobs.mockResolvedValue([])
    preview.mockResolvedValue({ preview_token: 'preview-1', expires_at: '2099-10-02T12:01:00Z', prefix_shape: 'SELECT * FROM orders WHERE id = ?', prefix_hash: 'hash', items: [{ id: '42', user: 'app', database: 'orders', duration_seconds: 75, query_hash: 'abc123', query_shape: 'SELECT * FROM orders WHERE id = ?' }] })
    cancelByPrefix.mockResolvedValue({ matched_count: 1, cancelled_count: 1, skipped_count: 0, failed_count: 0 })
    createLoop.mockResolvedValue({ id: 9 } as api.SessionLoopJob)
    stopLoop.mockResolvedValue({ ok: true })
  })

  it('以 live AWS topology 選定 physical node 並呈現 protected session', async () => {
    render(<SessionManagementPage />)
    await screen.findByText('Session Management')
    choose('DB Connection', 'Orders (mysql)')
    await waitFor(() => expect(listClusters).toHaveBeenCalledWith(7))
    choose('Cluster', 'orders (ap-northeast-1)')
    await waitFor(() => expect(getTopology).toHaveBeenCalledWith(7, 'ap-northeast-1', 'orders'))
    choose('Physical Node', 'orders-1 · writer')
    fireEvent.click(screen.getByRole('button', { name: 'Refresh Sessions' }))

    await waitFor(() => expect(readSessions).toHaveBeenCalledWith({ connection_id: 7, mode: 'aws', region: 'ap-northeast-1', cluster_id: 'orders', node_id: 'orders-1' }, expect.any(AbortSignal)))
    expect(await screen.findByText('SELECT * FROM orders')).toBeInTheDocument()
    expect(screen.getByText('tool-owned session')).toBeInTheDocument()
  })

  it('manual target 送出 host 與 port，由後端執行 policy validation', async () => {
    render(<SessionManagementPage />)
    await screen.findByText('Session Management')
    choose('DB Connection', 'Orders (mysql)')
    fireEvent.click(screen.getByRole('button', { name: 'Manual' }))
    fireEvent.change(screen.getByLabelText('Host'), { target: { value: 'db.internal' } })
    fireEvent.change(screen.getByLabelText('Port'), { target: { value: '3307' } })
    fireEvent.click(screen.getByRole('button', { name: 'Refresh Sessions' }))
    await waitFor(() => expect(readSessions).toHaveBeenCalledWith({ connection_id: 7, mode: 'manual', host: 'db.internal', port: 3307 }, expect.any(AbortSignal)))
  })

  it('初始 API 失敗時顯示可操作的錯誤狀態', async () => {
    listConnections.mockRejectedValue(new Error('offline'))
    render(<SessionManagementPage />)
    expect(await screen.findByText('Failed to load DB connections.')).toBeInTheDocument()
  })

  it('未配置 operations credential 時阻止查詢', async () => {
    listConnections.mockResolvedValue([{ id: 7, name: 'Orders', db_type: 'mysql', operations_configured: false }])
    render(<SessionManagementPage />)
    await screen.findByText('Session Management')
    choose('DB Connection', 'Orders (mysql)')
    expect(await screen.findByText(/Configure an operations credential/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Refresh Sessions' })).toBeDisabled()
  })

  it('Cancel Query 送出 target 與完整 expected identity，避免 session ID 重用誤殺', async () => {
    readSessions.mockResolvedValue({ items: [{ id: '42', user: 'app', database: 'orders', client: '10.0.0.8', state: 'Query', duration_seconds: 75, query: 'SELECT * FROM orders', query_hash: 'abc123', backend_start: '2026-10-02 12:00:00+00', protected: false }], truncated: false })
    render(<SessionManagementPage />)
    await screen.findByText('Session Management')
    choose('DB Connection', 'Orders (mysql)')
    await waitFor(() => expect(listClusters).toHaveBeenCalled())
    choose('Cluster', 'orders (ap-northeast-1)')
    await waitFor(() => expect(getTopology).toHaveBeenCalled())
    choose('Physical Node', 'orders-1 · writer')
    fireEvent.click(screen.getByRole('button', { name: 'Refresh Sessions' }))
    await screen.findByText('SELECT * FROM orders')
    fireEvent.click(screen.getByRole('button', { name: 'Cancel Query' }))
    const confirmButtons = screen.getAllByRole('button', { name: 'Cancel Query' })
    fireEvent.click(confirmButtons[confirmButtons.length - 1])

    await waitFor(() => expect(cancel).toHaveBeenCalledWith('42', {
      connection_id: 7, mode: 'aws', region: 'ap-northeast-1', cluster_id: 'orders', node_id: 'orders-1',
      expected: { user: 'app', database: 'orders', client: '10.0.0.8', query_hash: 'abc123', backend_start: '2026-10-02 12:00:00+00' },
    }))
    expect(await screen.findByText('Query cancellation requested.')).toBeInTheDocument()
  })

  it('protected session 與 read-only 使用者都不提供 destructive action', async () => {
    auth.mockReturnValue({ user: { permissions: ['db_sessions.read'] } } as ReturnType<typeof useAuth>)
    readSessions.mockResolvedValue({ items: [{ id: '42', user: 'app', duration_seconds: 1, query_hash: 'hash', protected: false }, { id: '43', user: 'rdsadmin', duration_seconds: 1, query_hash: 'hash2', protected: true, protected_reason: 'system session' }], truncated: false })
    render(<SessionManagementPage />)
    await screen.findByText('Session Management')
    choose('DB Connection', 'Orders (mysql)')
    await waitFor(() => expect(listClusters).toHaveBeenCalled())
    choose('Cluster', 'orders (ap-northeast-1)')
    await waitFor(() => expect(getTopology).toHaveBeenCalled())
    choose('Physical Node', 'orders-1 · writer')
    fireEvent.click(screen.getByRole('button', { name: 'Refresh Sessions' }))

    expect(await screen.findByText('Read only')).toBeInTheDocument()
    expect(screen.getByText('Protected')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Terminate' })).not.toBeInTheDocument()
  })

  it('mutation 失敗時保留確認視窗並顯示錯誤', async () => {
    readSessions.mockResolvedValue({ items: [{ id: '42', user: 'app', duration_seconds: 1, query_hash: 'hash', protected: false }], truncated: false })
    terminate.mockRejectedValue(new Error('offline'))
    render(<SessionManagementPage />)
    await screen.findByText('Session Management')
    choose('DB Connection', 'Orders (mysql)')
    await waitFor(() => expect(listClusters).toHaveBeenCalled())
    choose('Cluster', 'orders (ap-northeast-1)')
    await waitFor(() => expect(getTopology).toHaveBeenCalled())
    choose('Physical Node', 'orders-1 · writer')
    fireEvent.click(screen.getByRole('button', { name: 'Refresh Sessions' }))
    await screen.findByText('42')
    fireEvent.click(screen.getByRole('button', { name: 'Terminate' }))
    fireEvent.click(screen.getByRole('button', { name: 'Terminate Session' }))

    expect(await screen.findByText('Failed to modify the database session.')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Terminate Session' })).toBeInTheDocument()
  })

  it('target 切換時取消尚未完成的 session request，避免舊資料覆蓋新 target', async () => {
    readSessions.mockImplementation(() => new Promise(() => undefined))
    render(<SessionManagementPage />)
    await screen.findByText('Session Management')
    choose('DB Connection', 'Orders (mysql)')
    await waitFor(() => expect(listClusters).toHaveBeenCalled())
    choose('Cluster', 'orders (ap-northeast-1)')
    await waitFor(() => expect(getTopology).toHaveBeenCalled())
    choose('Physical Node', 'orders-1 · writer')
    fireEvent.click(screen.getByRole('button', { name: 'Refresh Sessions' }))
    await waitFor(() => expect(readSessions).toHaveBeenCalled())
    const signal = readSessions.mock.calls[0][1]

    fireEvent.click(screen.getByRole('button', { name: 'Manual' }))
    expect(signal?.aborted).toBe(true)
  })

  it('prefix preview token 可執行一次性 cancel 或建立有界 loop job', async () => {
    auth.mockReturnValue({ user: { permissions: ['db_sessions.read', 'db_sessions.kill', 'db_sessions.loop_kill'] } } as ReturnType<typeof useAuth>)
    render(<SessionManagementPage />)
    await screen.findByText('Session Management')
    choose('DB Connection', 'Orders (mysql)')
    await waitFor(() => expect(listClusters).toHaveBeenCalled())
    choose('Cluster', 'orders (ap-northeast-1)')
    await waitFor(() => expect(getTopology).toHaveBeenCalled())
    choose('Physical Node', 'orders-1 · writer')
    fireEvent.change(screen.getByLabelText('Prefix database'), { target: { value: 'orders' } })
    fireEvent.change(screen.getByLabelText('SQL prefix'), { target: { value: 'SELECT * FROM orders' } })
    fireEvent.click(screen.getByRole('button', { name: 'Preview matches' }))
    expect(await screen.findByText('1 matching sessions')).toBeInTheDocument()
    expect(preview).toHaveBeenCalledWith(expect.objectContaining({ database: 'orders', prefix: 'SELECT * FROM orders', minimum_age_seconds: 0 }))

    fireEvent.click(screen.getByRole('button', { name: 'Cancel once' }))
    fireEvent.click(screen.getByRole('button', { name: 'Cancel Queries' }))
    await waitFor(() => expect(cancelByPrefix).toHaveBeenCalledWith(expect.objectContaining({ preview_token: 'preview-1' })))

    fireEvent.click(screen.getByRole('button', { name: 'Preview matches' }))
    await screen.findByText('1 matching sessions')
    fireEvent.click(screen.getByRole('button', { name: 'Create loop job' }))
    fireEvent.click(screen.getByRole('button', { name: 'Create Job' }))
    await waitFor(() => expect(createLoop).toHaveBeenCalledWith(expect.objectContaining({ preview_token: 'preview-1', interval_seconds: 2, duration_seconds: 600, max_kills: 100 })))
  })

  it('Redis 不顯示 SQL prefix controls', async () => {
    listConnections.mockResolvedValue([{ id: 8, name: 'Cache', db_type: 'redis', operations_configured: true }])
    render(<SessionManagementPage />)
    await screen.findByText('Session Management')
    choose('DB Connection', 'Cache (redis)')
    expect(screen.queryByText('SQL Prefix Actions')).not.toBeInTheDocument()
  })

  it('loop_kill 權限可停止 active job，read 權限仍可查看 job', async () => {
    auth.mockReturnValue({ user: { permissions: ['db_sessions.read', 'db_sessions.loop_kill'] } } as ReturnType<typeof useAuth>)
    loopJobs.mockResolvedValue([{ id: 9, engine: 'mysql', node_id: 'orders-1', database_name: 'orders', prefix_shape: 'SELECT * FROM orders', status: 'running', kill_count: 2, max_kills: 100, interval_seconds: 2, duration_seconds: 600 } as api.SessionLoopJob])
    render(<SessionManagementPage />)
    expect(await screen.findByText('SELECT * FROM orders')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Stop' }))
    await waitFor(() => expect(stopLoop).toHaveBeenCalledWith(9))
  })

  it('auto refresh 使用選定週期，離頁時清除 timer', async () => {
    const setIntervalSpy = vi.spyOn(window, 'setInterval')
    const clearIntervalSpy = vi.spyOn(window, 'clearInterval')
    const view = render(<SessionManagementPage />)
    await screen.findByText('Session Management')
    choose('DB Connection', 'Orders (mysql)')
    await waitFor(() => expect(listClusters).toHaveBeenCalled())
    choose('Cluster', 'orders (ap-northeast-1)')
    await waitFor(() => expect(getTopology).toHaveBeenCalled())
    choose('Physical Node', 'orders-1 · writer')
    choose('Auto refresh', 'Every 5s')
    await waitFor(() => expect(setIntervalSpy).toHaveBeenCalledWith(expect.any(Function), 5000))

    view.unmount()
    expect(clearIntervalSpy).toHaveBeenCalled()
    setIntervalSpy.mockRestore(); clearIntervalSpy.mockRestore()
  })

  it('頁面進入 hidden 狀態時中止進行中的 refresh request', async () => {
    readSessions.mockImplementation(() => new Promise(() => undefined))
    render(<SessionManagementPage />)
    await screen.findByText('Session Management')
    choose('DB Connection', 'Orders (mysql)')
    await waitFor(() => expect(listClusters).toHaveBeenCalled())
    choose('Cluster', 'orders (ap-northeast-1)')
    await waitFor(() => expect(getTopology).toHaveBeenCalled())
    choose('Physical Node', 'orders-1 · writer')
    choose('Auto refresh', 'Every 2s')
    fireEvent.click(screen.getByRole('button', { name: 'Refresh Sessions' }))
    await waitFor(() => expect(readSessions).toHaveBeenCalled())
    const signal = readSessions.mock.calls[0][1]

    Object.defineProperty(document, 'hidden', { configurable: true, value: true })
    document.dispatchEvent(new Event('visibilitychange'))
    expect(signal?.aborted).toBe(true)
    Object.defineProperty(document, 'hidden', { configurable: true, value: false })
  })

  it('client-side filters 會縮小 session 結果', async () => {
    readSessions.mockResolvedValue({ items: [
      { id: '42', user: 'app', database: 'orders', state: 'Query', duration_seconds: 1, query_hash: 'a', protected: false },
      { id: '43', user: 'reporter', database: 'analytics', state: 'Sleep', duration_seconds: 1, query_hash: 'b', protected: false },
    ], truncated: false })
    render(<SessionManagementPage />)
    await screen.findByText('Session Management')
    choose('DB Connection', 'Orders (mysql)')
    await waitFor(() => expect(listClusters).toHaveBeenCalled())
    choose('Cluster', 'orders (ap-northeast-1)')
    await waitFor(() => expect(getTopology).toHaveBeenCalled())
    choose('Physical Node', 'orders-1 · writer')
    fireEvent.click(screen.getByRole('button', { name: 'Refresh Sessions' }))
    await screen.findByText('reporter')
    choose('Filter by database', 'orders')
    expect(screen.getByText('app')).toBeInTheDocument()
    expect(screen.queryByText('reporter')).not.toBeInTheDocument()
  })
})
