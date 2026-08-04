import { apiClient } from '@/shared/api/client'
import type { DBConnection } from '@/shared/types/dbConnection'
import type {
  QueryAccessScopeMode,
  QueryAccessEffect,
  QueryAccessTicketItem,
  Ticket,
  TicketDetail,
  TicketReviewResult,
  TicketStatus,
  TicketType,
  WorkflowDashboardSummary,
} from '@/shared/types/ticket'

type TicketsResponse = {
  tickets: Ticket[]
  total: number
  limit: number
  offset: number
}

type DBConnectionsResponse = {
  connections: DBConnection[]
}

type TicketDatabasesResponse = {
  databases: Array<{
    name: string
  }>
}

type CreateTicketPayload = {
  title: string
  description?: string | null
  sql_content: string
  ticket_type: TicketType
  db_connection_id?: number | null
  database_name?: string | null
  approved_duration_minutes?: number | null
  scope_mode?: QueryAccessScopeMode | null
  items?: Array<{
    database_name: string
    table_name?: string | null
  }>
  rules?: Array<{
    effect: QueryAccessEffect
    connection_id: number
    database_pattern: string
    table_pattern: string
  }>
}

type ReviewTicketPayload = {
  sql_content: string
  ticket_type: TicketType
  db_connection_id: number
  database_name: string
}

type ReviewTicketResponse = {
  passed: boolean
  results: TicketReviewResult[]
}

type ListTicketsParams = {
  status?: TicketStatus
  type?: TicketType
  keyword?: string
  ticketNo?: string
  title?: string
  submitter?: string
  from?: string
  to?: string
  limit?: number
  offset?: number
}

export async function listTickets(params: ListTicketsParams = {}) {
  const query = new URLSearchParams()
  if (params.status) {
    query.set('status', params.status)
  }
  if (params.type) {
    query.set('type', params.type)
  }
  if (params.keyword?.trim()) {
    query.set('q', params.keyword.trim())
  }
  if (params.ticketNo?.trim()) {
    query.set('ticket_no', params.ticketNo.trim())
  }
  if (params.title?.trim()) {
    query.set('title', params.title.trim())
  }
  if (params.submitter?.trim()) {
    query.set('submitter', params.submitter.trim())
  }
  if (params.from) {
    query.set('from', params.from)
  }
  if (params.to) {
    query.set('to', params.to)
  }
  if (params.limit != null) {
    query.set('limit', String(params.limit))
  }
  if (params.offset != null) {
    query.set('offset', String(params.offset))
  }

  const path = query.size > 0 ? `/tickets?${query.toString()}` : '/tickets'
  const response = await apiClient.get<TicketsResponse>(path)

  return {
    ...response,
    tickets: Array.isArray(response.tickets) ? response.tickets : [],
    total: typeof response.total === 'number' ? response.total : 0,
  }
}

export async function getTicket(id: string): Promise<TicketDetail> {
  return apiClient.get<TicketDetail>(`/tickets/${id}`).then((response) => ({
    ...response,
    executions: Array.isArray(response.executions) ? response.executions : [],
    review_results: Array.isArray(response.review_results) ? response.review_results : [],
    activity_logs: Array.isArray(response.activity_logs) ? response.activity_logs : [],
    scopes: Array.isArray(response.scopes) ? response.scopes : [],
    query_access_items: Array.isArray(response.query_access_items) ? response.query_access_items as QueryAccessTicketItem[] : [],
    export_request: response.export_request ?? null,
    workflow_participants: {
      reviewers: Array.isArray(response.workflow_participants?.reviewers) ? response.workflow_participants.reviewers : [],
      executors: Array.isArray(response.workflow_participants?.executors) ? response.workflow_participants.executors : [],
    },
    workflow_resolution_trace: response.workflow_resolution_trace ?? null,
  }))
}

export async function reviewTicketSQL(payload: ReviewTicketPayload) {
  return apiClient.post<ReviewTicketResponse>('/tickets/review', payload).then((response) => ({
    ...response,
    results: Array.isArray(response.results) ? response.results : [],
  }))
}

export async function createTicket(payload: CreateTicketPayload) {
  return apiClient.post<Ticket>('/tickets', payload)
}

export async function approveTicket(ticketRef: string | number, comment?: string) {
  return apiClient.post<Ticket>(`/tickets/${ticketRef}/approve`, {
    comment: comment?.trim() ? comment : null,
  })
}

export async function rejectTicket(ticketRef: string | number, reason: string) {
  return apiClient.post<Ticket>(`/tickets/${ticketRef}/reject`, {
    reason,
  })
}

export async function withdrawTicket(ticketRef: string | number, reason?: string) {
  return apiClient.post<Ticket>(`/tickets/${ticketRef}/withdraw`, {
    reason: reason?.trim() ? reason.trim() : null,
  })
}

export async function executeTicket(ticketRef: string | number, comment?: string) {
  return apiClient.post<Ticket>(`/tickets/${ticketRef}/execute`, {
    comment: comment?.trim() ? comment.trim() : null,
  })
}

export async function executeTicketStatement(ticketRef: string | number, executionID: number) {
  return apiClient.post<Ticket>(`/tickets/${ticketRef}/executions/${executionID}/execute`)
}

export async function stopTicketStatement(ticketRef: string | number, executionID: number) {
  return apiClient.post<void>(`/tickets/${ticketRef}/executions/${executionID}/stop`)
}

export async function revokeTicket(ticketRef: string | number) {
  return apiClient.post<Ticket>(`/tickets/${ticketRef}/revoke`)
}

export async function retryWorkflowResolution(ticketRef: string | number) {
  return apiClient.post<{ ticket: Ticket }>(`/tickets/${ticketRef}/retry-workflow-resolution`)
}

export async function retryWorkflowResolutionBatch(ticketIDs?: number[]) {
  return apiClient.post<{ results: Array<Record<string, unknown>> }>('/tickets/retry-workflow-resolution-batch', {
    ticket_ids: ticketIDs ?? [],
  })
}

export async function getWorkflowDashboardSummary() {
  return apiClient.get<{ summary: WorkflowDashboardSummary }>('/tickets/workflow-dashboard-summary')
}

export async function listConnections() {
  const response = await apiClient.get<DBConnectionsResponse>('/tickets/connections')

  return {
    ...response,
    connections: Array.isArray(response.connections) ? response.connections : [],
  }
}

export async function listTicketDatabases(connectionID: number) {
  const response = await apiClient.get<TicketDatabasesResponse>(`/tickets/connections/${connectionID}/databases`)

  return {
    ...response,
    databases: Array.isArray(response.databases) ? response.databases : [],
  }
}

export async function downloadTicketExport(path: string) {
  const response = await apiClient.download(path)
  const blob = await response.blob()
  const objectURL = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  const contentDisposition = response.headers.get('content-disposition')
  const filename = getDownloadFilename(contentDisposition) ?? 'export.csv'

  anchor.href = objectURL
  anchor.download = filename
  document.body.appendChild(anchor)
  anchor.click()
  document.body.removeChild(anchor)
  URL.revokeObjectURL(objectURL)
}

function getDownloadFilename(contentDisposition: string | null) {
  if (!contentDisposition) {
    return null
  }

  const utf8Match = contentDisposition.match(/filename\*=UTF-8''([^;]+)/i)
  if (utf8Match?.[1]) {
    return decodeURIComponent(utf8Match[1])
  }

  const basicMatch = contentDisposition.match(/filename="?([^"]+)"?/i)
  return basicMatch?.[1] ?? null
}
