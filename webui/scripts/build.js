const { spawnSync } = require('node:child_process')
const fs = require('node:fs')
const path = require('node:path')

const root = path.resolve(__dirname, '..')
const outDir = path.resolve(root, '../pkg/webui/dist')
const fallbackTemplate = path.resolve(root, 'public/build-fallback/index.html')

function writeFallback(message) {
  fs.rmSync(outDir, { recursive: true, force: true })
  fs.mkdirSync(outDir, { recursive: true })

  const fallback = fs.existsSync(fallbackTemplate)
    ? fs.readFileSync(fallbackTemplate, 'utf8')
    : '<!doctype html><title>Photo Backup Station</title><h1>Build failed</h1>'

  fs.writeFileSync(
    path.join(outDir, 'index.html'),
    fallback.replace(/{{reason}}/g, message)
  )
}

const result = spawnSync('pnpm', ['exec', 'vite', 'build'], {
  stdio: 'inherit',
  cwd: root,
  shell: false
})

if (result.status !== 0) {
  writeFallback('vite build failed during CI or local run.')
  process.exit(1)
}

if (!fs.existsSync(path.join(outDir, 'index.html'))) {
  writeFallback('vite build completed but did not emit index.html')
  process.exit(1)
}

process.exit(0)
