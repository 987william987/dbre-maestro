import { defineConfig, loadEnv } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, '.', '')
  const proxyTarget = env.VITE_API_PROXY_TARGET || 'http://localhost:8080'
  const usePolling = env.CHOKIDAR_USEPOLLING === 'true'

  return {
    plugins: [react()],
    resolve: {
      alias: { '@': '/src' },
    },
    build: {
      rollupOptions: {
        output: {
          manualChunks(id) {
            if (
              id.includes('/node_modules/@uiw/react-codemirror/') ||
              id.includes('/node_modules/@codemirror/') ||
              id.includes('/node_modules/@lezer/') ||
              id.includes('/node_modules/codemirror/')
            ) {
              return 'codemirror-vendor'
            }
            return undefined
          },
        },
      },
    },
    server: {
      host: '0.0.0.0',
      port: 5173,
      watch: {
        usePolling,
      },
      proxy: {
        '/api': {
          target: proxyTarget,
          changeOrigin: true,
        },
      },
    },
  }
})
