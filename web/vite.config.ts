import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// Build into the Go embed directory served by fastgen.
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: '../internal/ui/dist',
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/api': 'http://127.0.0.1:8180',
      '/healthz': 'http://127.0.0.1:8180',
    },
  },
})
