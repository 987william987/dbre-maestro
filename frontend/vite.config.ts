import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: { '@': '/src' },
  },
  server: {
    port: 5173,
    proxy: {
      '/setup': 'http://localhost:8080',
      '/auth': 'http://localhost:8080',
      '/tickets': 'http://localhost:8080',
      '/audit-logs': 'http://localhost:8080',
      '/db-connections': 'http://localhost:8080',
      '/exports': 'http://localhost:8080',
      '/health': 'http://localhost:8080',
    },
  },
})
