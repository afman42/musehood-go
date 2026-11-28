// Component tests untuk UI 6 tool gaya OpenCode (28 Nov 2026): banner
// pertanyaan kursi (ask_question) dan panel server MCP di Pengaturan.
// Interaksi komponen<->API diuji di backend Go (tools_opencode_test.go).
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { renderComponent } from './compile.mjs';

const noop = () => {};
const question = {
  id: 'q1', seatCode: 'M01', seatName: 'Plamo',
  text: 'Warna apa yang dipilih?', options: ['Merah', 'Biru'],
  createdAt: '2026-11-28T08:00:00.000Z',
};

test('QuestionBanner: teks pertanyaan, kursi penanya, dan tombol pilihan tampil', async () => {
  const html = await renderComponent('QuestionBanner', { questions: [question], onAnswer: noop });
  assert.match(html, /question-banner/);
  assert.match(html, /Plamo \(M01\) bertanya/);
  assert.match(html, /Warna apa yang dipilih\?/);
  assert.match(html, />Merah</);
  assert.match(html, />Biru</);
});

test('QuestionBanner: input jawaban bebas selalu tersedia', async () => {
  const html = await renderComponent('QuestionBanner', { questions: [question], onAnswer: noop });
  assert.match(html, /aria-label="Jawaban bebas"/);
  assert.match(html, /Kirim jawaban/);
});

test('QuestionBanner: tanpa pilihan tetap bisa menjawab bebas', async () => {
  const html = await renderComponent('QuestionBanner', {
    questions: [{ ...question, id: 'q2', options: undefined }],
    onAnswer: noop,
  });
  assert.ok(!html.includes('question-options'), 'blok pilihan tidak boleh tampil tanpa pilihan');
  assert.match(html, /aria-label="Jawaban bebas"/);
});

test('QuestionBanner: teks pertanyaan tidak di-render sebagai HTML mentah', async () => {
  const html = await renderComponent('QuestionBanner', {
    questions: [{ ...question, id: 'q3', text: '<script>alert(1)</script>' }],
    onAnswer: noop,
  });
  assert.ok(!html.includes('<script>alert'), 'tag script mentah tidak boleh lolos');
  assert.match(html, /&lt;script/);
});

test('SettingsMCP: form tambah server tampil dengan kolom nama/perintah/argumen/env', async () => {
  const html = await renderComponent('SettingsMCP', { notify: noop });
  assert.match(html, /Server MCP/);
  assert.match(html, /Nama server/);
  assert.match(html, /Perintah/);
  assert.match(html, /Simpan server MCP/);
});
