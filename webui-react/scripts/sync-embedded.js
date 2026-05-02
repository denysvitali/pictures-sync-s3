const fs = require('node:fs');
const path = require('node:path');

const root = process.cwd();
const sourceDist = path.join(root, 'dist');
const targetDist = path.join(root, '..', 'pkg', 'webui', 'dist');

if (!fs.existsSync(sourceDist)) {
  console.error(`Missing source dist: ${sourceDist}`);
  process.exit(1);
}

fs.rmSync(targetDist, { recursive: true, force: true });
fs.mkdirSync(targetDist, { recursive: true });
fs.cpSync(sourceDist, targetDist, { recursive: true });

console.log('Synced dist to', targetDist);
