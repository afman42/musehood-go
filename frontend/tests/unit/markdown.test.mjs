// Unit tests untuk parser Markdown subset (lib/markdown.ts).
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { parseInline, parseMarkdown } from '../../src/lib/markdown.ts';

test('parseInline: teks polos utuh', () => {
  assert.deepEqual(parseInline('halo dunia'), [{ type: 'text', text: 'halo dunia' }]);
});

test('parseInline: bold, italic, strike, kode inline', () => {
  const out = parseInline('**tebal** *miring* ~~coret~~ `kode`');
  assert.equal(out[0].type, 'strong');
  assert.equal(out[0].children[0].text, 'tebal');
  assert.equal(out[2].type, 'em');
  assert.equal(out[4].type, 'del');
  assert.equal(out[6].type, 'code');
  assert.equal(out[6].text, 'kode');
});

test('parseInline: *** jadi strong+em, ** bersarang *', () => {
  const both = parseInline('***penting***');
  assert.equal(both[0].type, 'strong');
  assert.equal(both[0].children[0].type, 'em');
  const nested = parseInline('**luar *dalam* akhir**');
  assert.equal(nested[0].type, 'strong');
  assert.equal(nested[0].children[1].type, 'em');
});

test('parseInline: penanda tak berpasangan tetap literal', () => {
  assert.deepEqual(parseInline('harga ** 5 ribu'), [{ type: 'text', text: 'harga ** 5 ribu' }]);
  assert.deepEqual(parseInline('a * b'), [{ type: 'text', text: 'a * b' }]);
});

test('parseInline: underscore di tengah kata bukan italic', () => {
  assert.deepEqual(parseInline('nama_file_saya'), [{ type: 'text', text: 'nama_file_saya' }]);
  const em = parseInline('ini _miring_ ya');
  assert.equal(em[1].type, 'em');
});

test('parseInline: tautan aman saja yang jadi link', () => {
  const ok = parseInline('[situs](https://example.com)');
  assert.deepEqual(ok, [{ type: 'link', text: 'situs', href: 'https://example.com' }]);
  const bad = parseInline('[klik](javascript:alert(1))');
  assert.equal(bad[0].type, 'text');
});

test('parseInline: escape backslash', () => {
  assert.deepEqual(parseInline('\\*bukan italic\\*'), [{ type: 'text', text: '*bukan italic*' }]);
});

test('parseMarkdown: heading, hr, paragraf', () => {
  const blocks = parseMarkdown('# Judul\n\nisi paragraf\n\n---\n\npenutup');
  assert.deepEqual(blocks.map(b => b.type), ['h', 'p', 'hr', 'p']);
  assert.equal(blocks[0].level, 1);
});

test('parseMarkdown: daftar ul dan ol', () => {
  const blocks = parseMarkdown('- satu\n- dua\n\n1. pertama\n2. kedua');
  assert.deepEqual(blocks.map(b => b.type), ['ul', 'ol']);
  assert.equal(blocks[0].items.length, 2);
  assert.equal(blocks[1].items.length, 2);
});

test('parseMarkdown: blockquote rekursif', () => {
  const blocks = parseMarkdown('> kutipan **tebal**\n> baris dua');
  assert.equal(blocks[0].type, 'quote');
  assert.equal(blocks[0].blocks[0].type, 'p');
});

test('parseMarkdown: blok kode literal tanpa parse inline', () => {
  const blocks = parseMarkdown('```js\nconst x = "**bukan bold**";\n```');
  assert.deepEqual(blocks, [{ type: 'code', text: 'const x = "**bukan bold**";', lang: 'js' }]);
});

test('parseMarkdown: contoh gaya balasan AI', () => {
  const sample = 'Dari sisi **Pelaksana**, berikut draftnya:\n\n---\n\n> **Produk:** *Airport Asli*\n> - *Baterai:* tahan lama\n> - *Material:* batu\n\nBeri tahu saya!';
  const blocks = parseMarkdown(sample);
  assert.deepEqual(blocks.map(b => b.type), ['p', 'hr', 'quote', 'p']);
  const quoteTypes = blocks[2].blocks.map(b => b.type);
  assert.deepEqual(quoteTypes, ['p', 'ul']);
});
