// Helper tes komponen: mengompilasi file .svelte ke JS server (SSR) dengan
// svelte/compiler, menulis hasilnya ke .tmp/, lalu me-render dengan
// svelte/server. Import relatif './format'/'./api' diarahkan ke file .ts
// asli (Node type-stripping), import './X.svelte' dikompilasi rekursif.
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { compile } from 'svelte/compiler';

const here = dirname(fileURLToPath(import.meta.url));
const libDir = join(here, '..', '..', 'src', 'lib');
const tmpDir = join(here, '.tmp');
const compiled = new Map();

export async function loadComponent(name) {
  if (compiled.has(name)) return compiled.get(name);
  await mkdir(tmpDir, { recursive: true });
  const source = await readFile(join(libDir, `${name}.svelte`), 'utf8');
  const { js } = compile(source, { generate: 'server', filename: `${name}.svelte` });
  let code = js.code;
  // Import komponen lain: './Toast.svelte' -> './Toast.mjs' (dikompilasi dulu).
  const nested = [...code.matchAll(/from\s+['"]\.\/([A-Za-z]+)\.svelte['"]/g)].map(m => m[1]);
  for (const child of nested) await loadComponent(child);
  code = code.replace(/from\s+['"]\.\/([A-Za-z]+)\.svelte['"]/g, "from './$1.mjs'");
  // Import helper .ts: './format' -> path relatif ke file asli dari .tmp.
  code = code.replace(/from\s+['"]\.\/(format|api|types|markdown)['"]/g, "from '../../../src/lib/$1.ts'");
  const out = join(tmpDir, `${name}.mjs`);
  await writeFile(out, code);
  const mod = await import(pathToFileURL(out).href);
  compiled.set(name, mod.default);
  return mod.default;
}

export async function renderComponent(name, props) {
  const { render } = await import('svelte/server');
  const component = await loadComponent(name);
  return render(component, { props }).body;
}
