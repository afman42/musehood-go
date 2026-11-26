// Syntax highlighting blok kode memakai @speed-highlight/core (CC0).
// Semua modul bahasa dimuat malas (dynamic import per bahasa) agar bundle
// awal tetap kecil; bahasa yang sudah dimuat disimpan di cache dan hasil
// token dirender sebagai span Svelte (bukan HTML mentah).
import { tokenizeWith, type ShjLanguageData, type ShjToken } from '@speed-highlight/core/tokenize';

export type CodeToken = { text: string; type?: ShjToken };

type LanguageModule = { default: ShjLanguageData };

const loaders: Record<string, () => Promise<LanguageModule>> = {
  asm: () => import('@speed-highlight/core/languages/asm.js'),
  bash: () => import('@speed-highlight/core/languages/bash.js'),
  bf: () => import('@speed-highlight/core/languages/bf.js'),
  c: () => import('@speed-highlight/core/languages/c.js'),
  css: () => import('@speed-highlight/core/languages/css.js'),
  csv: () => import('@speed-highlight/core/languages/csv.js'),
  diff: () => import('@speed-highlight/core/languages/diff.js'),
  docker: () => import('@speed-highlight/core/languages/docker.js'),
  git: () => import('@speed-highlight/core/languages/git.js'),
  go: () => import('@speed-highlight/core/languages/go.js'),
  html: () => import('@speed-highlight/core/languages/html.js'),
  http: () => import('@speed-highlight/core/languages/http.js'),
  ini: () => import('@speed-highlight/core/languages/ini.js'),
  java: () => import('@speed-highlight/core/languages/java.js'),
  js: () => import('@speed-highlight/core/languages/js.js'),
  jsdoc: () => import('@speed-highlight/core/languages/jsdoc.js'),
  json: () => import('@speed-highlight/core/languages/json.js'),
  'leanpub-md': () => import('@speed-highlight/core/languages/leanpub-md.js'),
  log: () => import('@speed-highlight/core/languages/log.js'),
  lua: () => import('@speed-highlight/core/languages/lua.js'),
  make: () => import('@speed-highlight/core/languages/make.js'),
  md: () => import('@speed-highlight/core/languages/md.js'),
  pl: () => import('@speed-highlight/core/languages/pl.js'),
  plain: () => import('@speed-highlight/core/languages/plain.js'),
  py: () => import('@speed-highlight/core/languages/py.js'),
  regex: () => import('@speed-highlight/core/languages/regex.js'),
  rs: () => import('@speed-highlight/core/languages/rs.js'),
  sql: () => import('@speed-highlight/core/languages/sql.js'),
  todo: () => import('@speed-highlight/core/languages/todo.js'),
  toml: () => import('@speed-highlight/core/languages/toml.js'),
  ts: () => import('@speed-highlight/core/languages/ts.js'),
  uri: () => import('@speed-highlight/core/languages/uri.js'),
  xml: () => import('@speed-highlight/core/languages/xml.js'),
  yaml: () => import('@speed-highlight/core/languages/yaml.js'),
};

export const LANGUAGE_NAMES = Object.keys(loaders);

// Nama bahasa yang dirujuk sebagai sub-bahasa oleh grammar lain
// (mis. jsdoc/todo di dalam js, js/css di dalam html).
const SUB_LANGUAGES = ['asm', 'bash', 'css', 'js', 'jsdoc', 'py', 'regex', 'todo'];

const ALIASES: Record<string, string> = {
  javascript: 'js',
  jsx: 'js',
  mjs: 'js',
  cjs: 'js',
  typescript: 'ts',
  tsx: 'ts',
  python: 'py',
  golang: 'go',
  sh: 'bash',
  shell: 'bash',
  zsh: 'bash',
  rust: 'rs',
  markdown: 'md',
  yml: 'yaml',
  dockerfile: 'docker',
  makefile: 'make',
  text: 'plain',
  txt: 'plain',
  perl: 'pl',
};

export function resolveLanguage(name: string): string | undefined {
  const key = name.trim().toLowerCase();
  if (!key) return undefined;
  if (loaders[key]) return key;
  return ALIASES[key];
}

const cache = new Map<string, ShjLanguageData>();
const pending = new Map<string, Promise<ShjLanguageData | undefined>>();

function loadLanguage(name: string): Promise<ShjLanguageData | undefined> {
  const hit = cache.get(name);
  if (hit) return Promise.resolve(hit);
  let promise = pending.get(name);
  if (!promise) {
    promise = loaders[name]()
      .then(mod => { cache.set(name, mod.default); return mod.default; })
      .catch(() => undefined);
    pending.set(name, promise);
  }
  return promise;
}

function tokenize(code: string, lang: ShjLanguageData): CodeToken[] | undefined {
  const languages: Record<string, ShjLanguageData> = {};
  for (const [name, data] of cache) languages[name] = data;
  const tokens: CodeToken[] = [];
  try {
    tokenizeWith(code, lang, (text, type) => { if (text) tokens.push({ text, type }); }, { languages });
  } catch {
    return undefined;
  }
  return tokens;
}

// Batas ukuran agar blok kode raksasa tidak membebani render.
const MAX_HIGHLIGHT_CHARS = 20000;

// Hasil highlight sinkron bila bahasanya sudah ada di cache (render ulang
// tanpa kedip); undefined bila belum dimuat atau bahasa tak dikenal.
export function cachedHighlight(code: string, langName: string): CodeToken[] | undefined {
  if (code.length > MAX_HIGHLIGHT_CHARS) return undefined;
  const name = resolveLanguage(langName);
  if (!name) return undefined;
  const lang = cache.get(name);
  if (!lang) return undefined;
  return tokenize(code, lang);
}

// Muat bahasa (dan sub-bahasanya) secara malas lalu tokenisasi.
export async function highlightCode(code: string, langName: string): Promise<CodeToken[] | undefined> {
  if (code.length > MAX_HIGHLIGHT_CHARS) return undefined;
  const name = resolveLanguage(langName);
  if (!name) return undefined;
  const lang = await loadLanguage(name);
  if (!lang) return undefined;
  await Promise.all(SUB_LANGUAGES.filter(sub => sub !== name).map(loadLanguage));
  return tokenize(code, lang);
}
