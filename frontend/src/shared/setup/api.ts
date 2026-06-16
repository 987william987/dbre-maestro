import { withApiPath } from '@/shared/api/client'

export type SetupStatusResponse = {
  setup_completed: boolean
}

export async function getSetupStatus(): Promise<SetupStatusResponse> {
  const response = await fetch(withApiPath('/setup/status'), {
    credentials: 'same-origin',
  })

  if (!response.ok) {
    throw new Error('Failed to load setup status')
  }

  const data = await response.json() as Partial<SetupStatusResponse>
  return {
    setup_completed: data.setup_completed === true,
  }
}
