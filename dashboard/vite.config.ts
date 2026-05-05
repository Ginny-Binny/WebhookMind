import { defineConfig } from 'vite'
import solid from 'vite-plugin-solid'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [solid(), tailwindcss()],
  server: {
    port: 3000,
    proxy: {
      '/api': 'http://localhost:8082',
      '/events': {
        target: 'http://localhost:8081',
      },
      '/webhook': 'http://localhost:8080',
    },
  },
})
