import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// Builds to web/dist like every other React app in the fleet, so the shared
// job-node-build can own this build. Getting the output into the Go embed
// directory (internal/ui/dist) is the consumer's job: CI downloads the dist
// artifact there, the Dockerfile copies it, and `make ui` does it locally.
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/api': 'http://127.0.0.1:8180',
      '/healthz': 'http://127.0.0.1:8180',
    },
  },
})
