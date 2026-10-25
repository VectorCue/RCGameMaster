import { fileURLToPath, URL } from 'node:url'

import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import vueDevTools from 'vite-plugin-vue-devtools'
import tailwindcss from '@tailwindcss/vite'

// https://vite.dev/config/
export default defineConfig({
  plugins: [
    vue(),
    vueDevTools(),
    tailwindcss(),
  ],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    host: '127.0.0.1',
    port: 2005,
    strictPort: true,
    proxy: {
      '/api': 'http://127.0.0.1:2006',
      '/auth': 'http://127.0.0.1:2006',
      '/rest': 'http://127.0.0.1:2006',
      '/healthcheck': 'http://127.0.0.1:2006',
    },
  },
})
