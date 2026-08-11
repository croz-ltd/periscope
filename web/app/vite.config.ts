import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { mockApi } from './mock/plugin'

// The Go binary embeds web/dist via `//go:embed all:dist`, so the build MUST
// write index.html + assets into ../dist (relative to this web/app dir).
//
// `vite --mode mock` (npm run dev:mock) adds a dev-server middleware that answers
// /api/* from a synthetic fleet, so the UI runs with no cluster and no Go server.
// The mock is a plugin, never imported by src/, so a production build cannot pick
// it up: mode is the only switch.
export default defineConfig(({ mode }) => ({
  base: './',
  plugins: [react(), ...(mode === 'mock' ? [mockApi()] : [])],
  build: {
    outDir: '../dist',
    emptyOutDir: true,
  },
  server: {
    // In mock mode there is no Go server to proxy to, and leaving the proxy on
    // races the mock middleware for /api.
    proxy: mode === 'mock' ? undefined : { '/api': 'http://localhost:8080' },
  },
}))
