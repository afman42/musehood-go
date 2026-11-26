// Unit tests untuk lazy-load syntax highlighting (lib/highlight.ts).
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { cachedHighlight, highlightCode, LANGUAGE_NAMES, resolveLanguage } from '../../src/lib/highlight.ts';

test('resolveLanguage: nama langsung, alias, dan tak dikenal', () => {
  assert.equal(resolveLanguage('js'), 'js');
  assert.equal(resolveLanguage('JavaScript'), 'js');
  assert.equal(resolveLanguage('python'), 'py');
  assert.equal(resolveLanguage('golang'), 'go');
  assert.equal(resolveLanguage('sh'), 'bash');
  assert.equal(resolveLanguage(''), undefined);
  assert.equal(resolveLanguage('cobol'), undefined);
});

test('semua 34 bahasa terdaftar sebagai loader malas', () => {
  assert.equal(LANGUAGE_NAMES.length, 34);
  for (const name of ['go', 'py', 'rs', 'sql', 'yaml', 'docker', 'md', 'ts']) {
    assert.ok(LANGUAGE_NAMES.includes(name), `bahasa ${name} hilang`);
  }
});

test('highlightCode: tokenisasi JS menghasilkan kwd dan str', async () => {
  const tokens = await highlightCode('const s = "halo"; // komentar', 'javascript');
  assert.ok(tokens, 'token harus ada');
  const kwd = tokens.find(t => t.type === 'kwd');
  assert.equal(kwd?.text, 'const');
  const str = tokens.find(t => t.type === 'str');
  assert.equal(str?.text, '"halo"');
  const cmnt = tokens.find(t => t.type === 'cmnt');
  assert.match(cmnt?.text ?? '', /komentar/);
});

test('highlightCode: bahasa tak dikenal -> undefined', async () => {
  assert.equal(await highlightCode('x = 1', 'cobol'), undefined);
  assert.equal(await highlightCode('x = 1', ''), undefined);
});

test('cachedHighlight: sinkron setelah bahasa termuat', async () => {
  await highlightCode('print(1)', 'py');
  const tokens = cachedHighlight('print(1)', 'python');
  assert.ok(tokens && tokens.length > 0);
});

test('highlightCode: kode raksasa dilewati demi performa', async () => {
  const big = 'x'.repeat(20001);
  assert.equal(await highlightCode(big, 'js'), undefined);
});
