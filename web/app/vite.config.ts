import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// The Go binary embeds web/dist via `//go:embed all:dist`, so the build MUST
// write index.html + assets into ../dist (relative to this web/app dir).
export default defineConfig({
  base: './',
  plugins: [react()],
  build: {
    outDir: '../dist',
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
    },
  },
})
