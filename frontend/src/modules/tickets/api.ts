import { apiClient } from '@/shared/api/client'
import type { DBConnection } from '@/shared/types/dbConnection'
import type { Ticket, TicketDetail, TicketStatus, TicketType } from '@/shared/types/ticket'

type TicketsResponse = {
  tickets: Ticket[]
  limit: number
  offset: number
}

type DBConnectionsResponse = {
  connections: DBConnection[]
}

type CreateTicketPayload = {
  title: string
  description?: string | null
  sql_content: string
  ticket_type: TicketType
  db_connection_id?: number | null
}

export async function listTickets(status?: TicketStatus) {
  const query = new URLSearchParams()
  if (status) {
    query.set('status', status)
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
    scopes: Array.isArray(response.scopes) ? response.scopes : [],
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
  const response = await apiClient.get<DBConnectionsResponse>('/db-connections')

  return {
    ...response,
    connections: Array.isArray(response.connections) ? response.connections : [],
  }
}
