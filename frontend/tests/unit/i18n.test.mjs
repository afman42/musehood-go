// Unit tests i18n.svelte.ts: paritas kunci ID/EN, fallback kunci hilang,
// dan persistensi pilihan bahasa. Modul dikompilasi dengan svelte/compiler
// (pola sama seperti chat.test.mjs) karena memakai $state.
import assert from 'node:assert/strict';
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { stripTypeScriptTypes } from 'node:module';
import { dirname, join } from 'node:path';
import test from 'node:test';
import { fileURLToPath } from 'node:url';
import { compileModule } from 'svelte/compiler';

const here = dirname(fileURLToPath(import.meta.url));
const libDir = join(here, '..', '..', 'src', 'lib');
const tmpDir = join(here, '.tmp');
mkdirSync(tmpDir, { recursive: true });
const source = readFileSync(join(libDir, 'i18n.svelte.ts'), 'utf8');
const compiled = compileModule(stripTypeScriptTypes(source), { filename: 'i18n.svelte.js' }).js.code;
writeFileSync(join(tmpDir, 'i18n.mjs'), compiled);

// Stub localStorage sebelum modul dimuat (loadLocale dipanggil saat impor).
const store = new Map();
globalThis.localStorage = {
  getItem: k => (store.has(k) ? store.get(k) : null),
  setItem: (k, v) => store.set(k, String(v)),
  removeItem: k => store.delete(k),
};
const { i18n, setLocale, t } = await import('./.tmp/i18n.mjs');

// Kunci + nilai kamus diekstrak dari sumber agar paritas teruji persis
// terhadap kamus asli (termasuk kunci yang nilai EN-nya sama dengan kunci,
// mis. `unread`).
const entries = [...source.matchAll(/^\s{2}(\w+): \{ id: '((?:[^'\\]|\\.)*)', en: '((?:[^'\\]|\\.)*)' \}/gm)]
  .map(m => ({ key: m[1], id: m[2], en: m[3] }));
const keys = entries.map(e => e.key);

test('i18n: kamus tidak kosong dan kunci unik', () => {
  assert.ok(keys.length >= 50, `hanya ${keys.length} kunci`);
  assert.equal(new Set(keys).size, keys.length);
});

test('i18n: semua kunci punya terjemahan ID dan EN (paritas)', () => {
  for (const entry of entries) {
    assert.ok(entry.id.length > 0, `${entry.key} ID kosong`);
    assert.ok(entry.en.length > 0, `${entry.key} EN kosong`);
    setLocale('id');
    assert.equal(t(entry.key), entry.id, `${entry.key} ID tidak cocok`);
    setLocale('en');
    assert.equal(t(entry.key), entry.en, `${entry.key} EN tidak cocok`);
  }
});

test('i18n: kunci tidak dikenal jatuh ke kunci itu sendiri', () => {
  setLocale('id');
  assert.equal(t('kunciYangTidakAda'), 'kunciYangTidakAda');
  setLocale('en');
  assert.equal(t('kunciYangTidakAda'), 'kunciYangTidakAda');
});

test('i18n: setLocale mengganti bahasa aktif + tersimpan', () => {
  setLocale('en');
  assert.equal(i18n.locale, 'en');
  assert.equal(t('settings'), 'Settings');
  assert.equal(store.get('musehood-locale'), 'en');
  setLocale('id');
  assert.equal(i18n.locale, 'id');
  assert.equal(t('settings'), 'Pengaturan');
  assert.equal(store.get('musehood-locale'), 'id');
});
