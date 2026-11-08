import react from '@vitejs/plugin-react'
import { msw } from 'msw/vite'
import { defineConfig } from 'vite'
import pkg from './package.json' with { type: 'json' }

const api = process.env.TASKLANE_API ?? 'http://localhost:8080'

export default defineConfig({
  plugins: [react(), msw()],
  define: {
    __APP_VERSION__: JSON.stringify(pkg.version),
  },
  server: {
    proxy: {
      '/api': api,
      '/healthz': api,
    },
  },
})
