const { defineConfig } = require('vite')
const react = require('@vitejs/plugin-react')

module.exports = defineConfig({
  plugins: [react()],
  base: './',
  build: {
    outDir: '../pkg/webui/dist',
    emptyOutDir: true,
    assetsDir: 'static',
    rollupOptions: {
      output: {
        entryFileNames: 'static/js/[name]-[hash].js',
        chunkFileNames: 'static/js/[name]-[hash].js',
        assetFileNames: 'static/[ext]/[name]-[hash][extname]'
      }
    }
  }
})
