import { apiClient } from '@/shared/api/client'
import type { DBConnection } from '@/shared/types/dbConnection'
import type { Ticket, TicketStatus, TicketType } from '@/shared/types/ticket'

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
  return apiClient.get<TicketsResponse>(path)
}

export async function getTicket(id: string) {
  return apiClient.get<Ticket>(`/tickets/${id}`)
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

export async function listConnections() {
  return apiClient.get<DBConnectionsResponse>('/db-connections')
}
