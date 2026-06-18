export const MAESTRO_REALTIME_EVENT = 'maestro:realtime'

export type MaestroRealtimeEvent =
  | {
      event: 'ticket.updated'
      data: {
        ticket_id: number
        status?: string
      } | null
    }
  | {
      event: 'notification.created'
      data: unknown
    }
  | {
      event: string
      data: unknown
    }
