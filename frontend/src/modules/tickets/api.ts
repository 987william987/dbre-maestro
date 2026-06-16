import { apiClient } from '@/shared/api/client'
import type { DBConnection } from '@/shared/types/dbConnection'
import type { Ticket, TicketDetail, TicketReviewResult, TicketStatus, TicketType } from '@/shared/types/ticket'

type TicketsResponse = {
  tickets: Ticket[]
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

export async function listTickets(status?: TicketStatus, limit?: number, offset?: number) {
  const query = new URLSearchParams()
  if (status) {
    query.set('status', status)
  }
  if (limit != null) {
    query.set('limit', String(limit))
  }
  if (offset != null) {
    query.set('offset', String(offset))
  }

  const path = query.size > 0 ? `/tickets?${query.toString()}` : '/tickets'
  const response = await apiClient.get<TicketsResponse>(path)

  return {
    ...response,
    tickets: Array.isArray(response.tickets) ? response.tickets : [],
  }
}

export async function getTicket(id: string) {
  return apiClient.get<TicketDetail>(`/tickets/${id}`).then((response) => ({
    ...response,
    executions: Array.isArray(response.executions) ? response.executions : [],
    review_results: Array.isArray(response.review_results) ? response.review_results : [],
    scopes: Array.isArray(response.scopes) ? response.scopes : [],
    export_request: response.export_request ?? null,
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

export async function approveTicket(id: number, comment?: string) {
  return apiClient.post<Ticket>(`/tickets/${id}/approve`, {
    comment: comment?.trim() ? comment : null,
  })
}

export async function rejectTicket(id: number, reason: string) {
  return apiClient.post<Ticket>(`/tickets/${id}/reject`, {
    reason,
  })
}

export async function requestExecution(id: number) {
  return apiClient.post<Ticket>(`/tickets/${id}/request-execution`)
}

export async function executeTicket(id: number) {
  return apiClient.post<Ticket>(`/tickets/${id}/execute`)
}

export async function revokeTicket(id: number) {
  return apiClient.post<Ticket>(`/tickets/${id}/revoke`)
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
