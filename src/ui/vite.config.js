import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    port: 8008,
    host: '0.0.0.0',
    proxy: {
      '/api': {
        target: 'http://localhost:8009',
        changeOrigin: true,
      }
    }
  },
  build: {
    outDir: './dist',
    emptyOutDir: true,
    sourcemap: false,
    minify: 'terser'
  }
})
