export type SetupStatusResponse = {
  setup_completed: boolean
}

export async function getSetupStatus(): Promise<SetupStatusResponse> {
  const response = await fetch('/setup/status', {
    credentials: 'same-origin',
  })

  if (!response.ok) {
    throw new Error('讀取 setup 狀態失敗')
  }

  const data = await response.json() as Partial<SetupStatusResponse>
  return {
    setup_completed: data.setup_completed === true,
  }
}
