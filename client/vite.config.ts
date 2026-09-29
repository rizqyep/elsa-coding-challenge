import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// Dev server proxies to the stack; nginx does the same routing in the full stack (task-25).
const api = process.env.API_TARGET ?? 'http://localhost:8080'
const ws = process.env.WS_TARGET ?? api.replace(/^http/, 'ws')

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': { target: api },
      '/ws': { target: ws, ws: true },
    },
  },
})
