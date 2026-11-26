// Helper tes komponen: mengompilasi file .svelte ke JS server (SSR) dengan
// svelte/compiler, menulis hasilnya ke .tmp/, lalu me-render dengan
// svelte/server. Import relatif './format'/'./api' diarahkan ke file .ts
// asli (Node type-stripping), import './X.svelte' dikompilasi rekursif.
import { access, mkdir, readFile, writeFile } from 'node:fs/promises';
import { stripTypeScriptTypes } from 'node:module';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { compile, compileModule } from 'svelte/compiler';

const here = dirname(fileURLToPath(import.meta.url));
const libDir = join(here, '..', '..', 'src', 'lib');
const tmpDir = join(here, '.tmp');
const compiled = new Map();
const compiledModules = new Set();

// Modul runes './i18n.svelte' (file i18n.svelte.ts) dikompilasi dengan
// compileModule agar bisa diimpor komponen hasil kompilasi SSR.
async function loadRunesModule(name) {
  if (compiledModules.has(name)) return;
  compiledModules.add(name);
  const source = await readFile(join(libDir, `${name}.svelte.ts`), 'utf8');
  const { js } = compileModule(stripTypeScriptTypes(source), { filename: `${name}.svelte.ts` });
  let code = js.code;
  const nested = [...code.matchAll(/from\s+['"]\.\/([A-Za-z0-9]+)\.svelte['"]/g)].map(m => m[1]);
  for (const child of nested) await loadNested(child);
  code = code.replace(/from\s+['"]\.\/([A-Za-z0-9]+)\.svelte['"]/g, "from './$1.mjs'");
  code = code.replace(/from\s+['"]\.\/(format|api|types|markdown|highlight)['"]/g, "from '../../../src/lib/$1.ts'");
  await writeFile(join(tmpDir, `${name}.mjs`), code);
}

async function loadNested(name) {
  try {
    await access(join(libDir, `${name}.svelte.ts`));
    await loadRunesModule(name);
  } catch {
    await loadComponent(name);
  }
}

export async function loadComponent(name) {
  if (compiled.has(name)) return compiled.get(name);
  await mkdir(tmpDir, { recursive: true });
  const source = await readFile(join(libDir, `${name}.svelte`), 'utf8');
  const { js } = compile(source, { generate: 'server', filename: `${name}.svelte` });
  let code = js.code;
  // Import komponen lain: './Toast.svelte' -> './Toast.mjs' (dikompilasi dulu).
  const nested = [...code.matchAll(/from\s+['"]\.\/([A-Za-z0-9]+)\.svelte['"]/g)].map(m => m[1]);
  for (const child of nested) await loadNested(child);
  code = code.replace(/from\s+['"]\.\/([A-Za-z0-9]+)\.svelte['"]/g, "from './$1.mjs'");
  // Import helper .ts: './format' -> path relatif ke file asli dari .tmp.
  code = code.replace(/from\s+['"]\.\/(format|api|types|markdown|highlight)['"]/g, "from '../../../src/lib/$1.ts'");
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
