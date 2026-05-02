const fs = require('node:fs');
const path = require('node:path');

const root = process.cwd();
const repoRoot = path.resolve(root, '..');
const sourceTemplate = path.join(repoRoot, 'pkg', 'webui', 'templates', 'spa.html');
const sourceStatic = path.join(repoRoot, 'pkg', 'webui', 'static');
const distDir = path.join(root, 'dist');
const distIndex = path.join(distDir, 'index.html');
const distStatic = path.join(distDir, 'static');

if (!fs.existsSync(sourceTemplate)) {
  console.error(`Missing template: ${sourceTemplate}`);
  process.exit(1);
}

if (!fs.existsSync(sourceStatic)) {
  console.error(`Missing static directory: ${sourceStatic}`);
  process.exit(1);
}

fs.rmSync(distDir, { recursive: true, force: true });
fs.mkdirSync(distDir, { recursive: true });

fs.copyFileSync(sourceTemplate, distIndex);
fs.mkdirSync(distStatic, { recursive: true });
fs.cpSync(sourceStatic, distStatic, { recursive: true });

const entries = fs.readdirSync(distDir);
if (entries.length === 0) {
  console.error('Build failed: dist output is empty');
  process.exit(1);
}

console.log('Built webui assets to', distDir);
