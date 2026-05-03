import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'path'

export default defineConfig({
  plugins: [react()],
  base: './',
  server: {
    host: 'localhost',
    port: 5173,
  },
  build: {
    outDir: path.resolve(__dirname, '..', 'web-dist'),
    emptyOutDir: true,
  },
})
