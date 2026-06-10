import { defineConfig, loadEnv, type ProxyOptions } from 'vite'
import react from '@vitejs/plugin-react'

type ProxyRequest = {
  method?: string
  url?: string
  headers: Record<string, string | string[] | undefined>
}

function shouldBypassToSpa(req: ProxyRequest) {
  const accept = req.headers.accept ?? ''
  const destination = req.headers['sec-fetch-dest']

  return (
    req.method === 'GET' &&
    typeof accept === 'string' &&
    accept.includes('text/html') &&
    (destination === undefined || destination === 'document')
  )
}

function createApiProxy(target: string): ProxyOptions {
  return {
    target,
    changeOrigin: true,
    bypass(req) {
      if (shouldBypassToSpa(req)) {
        return req.url
      }

      return undefined
    },
  }
}

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, '.', '')
  const proxyTarget = env.VITE_API_PROXY_TARGET || 'http://localhost:8080'
  const usePolling = env.CHOKIDAR_USEPOLLING === 'true'

  return {
    plugins: [react()],
    resolve: {
      alias: { '@': '/src' },
    },
    server: {
      host: '0.0.0.0',
      port: 5173,
      watch: {
        usePolling,
      },
      proxy: {
        '/setup': createApiProxy(proxyTarget),
        '/auth': createApiProxy(proxyTarget),
        '/tickets': createApiProxy(proxyTarget),
        '/audit-logs': createApiProxy(proxyTarget),
        '/db-connections': createApiProxy(proxyTarget),
        '/exports': createApiProxy(proxyTarget),
        '/health': createApiProxy(proxyTarget),
      },
    },
  }
})
