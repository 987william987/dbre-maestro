import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { GlobalErrorBoundary } from '@/app/errors/AppErrorBoundary'
import { AppProviders } from '@/app/providers/AppProviders'
import './index.css'
import App from './App.tsx'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <GlobalErrorBoundary>
      <AppProviders>
        <App />
      </AppProviders>
    </GlobalErrorBoundary>
  </StrictMode>,
)
