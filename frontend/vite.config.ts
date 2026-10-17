/// <reference types="vitest" />
import { defineConfig, loadEnv } from 'vite'
import react from '@vitejs/plugin-react'

// The browser talks to the Go gateway only. In development the gateway is reached through this proxy,
// so the page and the API share one origin. Set VITE_API_BASE to call a deployed gateway directly instead.
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')
  const target = env.VITE_PROXY_TARGET || 'http://127.0.0.1:8080'
  const proxy = {
    '/api': { target, changeOrigin: true },
    '/health': { target, changeOrigin: true },
    '/ready': { target, changeOrigin: true },
  }
  return {
    plugins: [react()],
    server: { port: 5173, proxy },
    preview: { proxy },
    // the e2e/ folder belongs to Playwright, not Vitest
    test: { exclude: ['**/node_modules/**', 'e2e/**'] },
  }
})
