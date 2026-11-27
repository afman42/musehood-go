// Unit tests untuk pintasan perintah /slash (fitur 34) dan toggle rating (fitur 30).
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { parseSlashCommand, slashMessageText, slashNeedsArg, SLASH_COMMANDS, SLASH_HELP_TEXT } from '../../src/lib/slash.ts';
import { nextRating } from '../../src/lib/rating.ts';

test('parseSlashCommand: perintah dikenal dengan dan tanpa argumen', () => {
  assert.deepEqual(parseSlashCommand('/ringkas'), { command: 'ringkas', arg: '' });
  assert.deepEqual(parseSlashCommand('/terjemah hello world'), { command: 'terjemah', arg: 'hello world' });
  assert.deepEqual(parseSlashCommand('  /kode buat fungsi tambah  '), { command: 'kode', arg: 'buat fungsi tambah' });
  assert.deepEqual(parseSlashCommand('/gambar kucing oren'), { command: 'gambar', arg: 'kucing oren' });
  assert.deepEqual(parseSlashCommand('/bantu'), { command: 'bantu', arg: '' });
  assert.deepEqual(parseSlashCommand('/TERJEMAH halo'), { command: 'terjemah', arg: 'halo' });
});

test('parseSlashCommand: bukan perintah → null', () => {
  assert.equal(parseSlashCommand('halo semua'), null);
  assert.equal(parseSlashCommand('/'), null);
  assert.equal(parseSlashCommand('/tidakkenal sesuatu'), null);
  assert.equal(parseSlashCommand('kata /terjemah di tengah'), null);
  assert.equal(parseSlashCommand(''), null);
});

test('slashMessageText: transformasi ke pesan pemilik', () => {
  assert.match(slashMessageText({ command: 'ringkas', arg: '' }, 'Muse', 'Plamo'), /^@Muse Ringkas diskusi/);
  assert.equal(slashMessageText({ command: 'terjemah', arg: 'hello' }, 'Muse', 'Plamo'), '@Muse Terjemahkan ke Bahasa Indonesia: hello');
  assert.match(slashMessageText({ command: 'kode', arg: 'fungsi tambah' }, 'Muse', 'Plamo'), /^@Plamo Tulis kode/);
  assert.equal(slashMessageText({ command: 'gambar', arg: 'kucing oren' }, 'Muse', 'Plamo'), 'Gambar: kucing oren');
  assert.equal(slashMessageText({ command: 'bantu', arg: '' }, 'Muse', 'Plamo'), null);
  assert.equal(slashMessageText({ command: 'terjemah', arg: '' }, 'Muse', 'Plamo'), null);
  assert.equal(slashMessageText({ command: 'kode', arg: '' }, 'Muse', 'Plamo'), null);
  assert.equal(slashMessageText({ command: 'gambar', arg: '' }, 'Muse', 'Plamo'), null);
});

test('slashNeedsArg dan daftar perintah', () => {
  assert.equal(slashNeedsArg('terjemah'), true);
  assert.equal(slashNeedsArg('kode'), true);
  assert.equal(slashNeedsArg('gambar'), true);
  assert.equal(slashNeedsArg('ringkas'), false);
  assert.equal(slashNeedsArg('bantu'), false);
  assert.equal(SLASH_COMMANDS.length, 5);
  for (const item of SLASH_COMMANDS) {
    assert.ok(SLASH_HELP_TEXT.includes(item.usage), item.usage);
  }
});

test('nextRating: toggle 👍/👎', () => {
  assert.equal(nextRating(0, 1), 1);
  assert.equal(nextRating(1, 1), 0);
  assert.equal(nextRating(0, -1), -1);
  assert.equal(nextRating(-1, -1), 0);
  assert.equal(nextRating(1, -1), -1);
  assert.equal(nextRating(-1, 1), 1);
  assert.equal(nextRating(1, 0), 0);
  assert.equal(nextRating(0, 5), 0);
});
