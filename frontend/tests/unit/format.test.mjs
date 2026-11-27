// Unit tests untuk helper format frontend (dijalankan Node --test dengan
// type-stripping bawaan Node 24, tanpa build step).
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { attachmentLabel, highlightParts, humanSize, isImageAttachment, splitMentions } from '../../src/lib/format.ts';

test('humanSize: batas byte/KB/MB', () => {
  assert.equal(humanSize(0), '0 B');
  assert.equal(humanSize(512), '512 B');
  assert.equal(humanSize(999), '999 B');
  assert.equal(humanSize(1023), '1023 B');
  assert.equal(humanSize(1024), '1.0 KB');
  assert.equal(humanSize(1536), '1.5 KB');
  assert.equal(humanSize(1024 * 1024 - 1), '1024.0 KB');
  assert.equal(humanSize(1024 * 1024), '1.0 MB');
  assert.equal(humanSize(5.5 * 1024 * 1024), '5.5 MB');
});

test('humanSize: batas MB/GB', () => {
  assert.equal(humanSize(1024 * 1024 * 1024 - 1), '1024.0 MB');
  assert.equal(humanSize(1024 * 1024 * 1024), '1.0 GB');
  assert.equal(humanSize(2.5 * 1024 * 1024 * 1024), '2.5 GB');
});

test('highlightParts: sorotan case-insensitive, semua kemunculan', () => {
  // Segmen teks kosong di awal muncul bila kecocokan di posisi 0
  // (tidak terlihat saat dirender sebagai <mark>).
  assert.deepEqual(highlightParts('Halo halo HALO', 'halo'), [
    '', { hit: 'Halo' }, ' ', { hit: 'halo' }, ' ', { hit: 'HALO' }, '',
  ]);
  assert.deepEqual(highlightParts('satu dua', 'dua'), ['satu ', { hit: 'dua' }, '']);
  assert.deepEqual(highlightParts('teks', '  teks  '), ['', { hit: 'teks' }, '']);
});

test('highlightParts: tanpa kecocokan atau query kosong utuh', () => {
  assert.deepEqual(highlightParts('teks biasa', 'zzz'), ['teks biasa']);
  assert.deepEqual(highlightParts('teks biasa', ''), ['teks biasa']);
  assert.deepEqual(highlightParts('teks biasa', '   '), ['teks biasa']);
  assert.deepEqual(highlightParts('', 'x'), ['']);
});

test('isImageAttachment: hanya mime image/*', () => {
  assert.equal(isImageAttachment({ mime: 'image/png' }), true);
  assert.equal(isImageAttachment({ mime: 'image/jpeg' }), true);
  assert.equal(isImageAttachment({ mime: 'application/pdf' }), false);
  assert.equal(isImageAttachment({ mime: 'text/plain' }), false);
  assert.equal(isImageAttachment({ mime: '' }), false);
});

test('attachmentLabel: prioritas teks > gambar > lampiran > kosong', () => {
  assert.equal(attachmentLabel({ text: 'halo' }), 'halo');
  assert.equal(attachmentLabel({ text: 'halo', imageUrl: '/images/x.png' }), 'halo');
  assert.equal(attachmentLabel({ text: '', imageUrl: '/images/x.png' }), '[gambar]');
  assert.equal(attachmentLabel({ text: '', attachment: { name: 'laporan.pdf' } }), '[lampiran: laporan.pdf]');
  assert.equal(attachmentLabel({ text: '', imageUrl: '/images/x.png', attachment: { name: 'a.pdf' } }), '[gambar]');
  assert.equal(attachmentLabel({ text: '' }), '');
});

const seats = [
  { code: 'M01', name: 'Plamo' },
  { code: 'M02', name: 'GLM' },
  { code: 'C01', name: 'Kopi Susu' },
];

test('splitMentions: nama dan kode kursi dikenali', () => {
  const segments = splitMentions('Halo @Plamo dan @M02!', seats);
  assert.deepEqual(segments.map(s => s.text), ['Halo ', '@Plamo', ' dan ', '@M02', '!']);
  assert.equal(segments[1].seat.code, 'M01');
  assert.equal(segments[3].seat.code, 'M02');
});

test('splitMentions: case-insensitive, nama berspasi, awal teks', () => {
  const segments = splitMentions('@plamo tolong cek, cc @Kopi Susu', seats);
  assert.equal(segments[0].seat.code, 'M01');
  assert.equal(segments.at(-1).seat.code, 'C01');
  assert.equal(segments.at(-1).text, '@Kopi Susu');
});

test('splitMentions: email dan nama tak dikenal bukan mention', () => {
  const segments = splitMentions('kirim ke a@Plamo.com atau @Siapa saja', seats);
  assert.equal(segments.length, 1);
  assert.equal(segments[0].seat, undefined);
});

test('splitMentions: tanpa kursi atau teks kosong utuh', () => {
  assert.deepEqual(splitMentions('@Plamo', []), [{ text: '@Plamo' }]);
  assert.deepEqual(splitMentions('', seats), [{ text: '' }]);
});

test('splitMentions: nama mirip awalan — label terpanjang menang', () => {
  const mirip = [
    { code: 'M02', name: 'GLM' },
    { code: 'C02', name: 'GLM Pro' },
  ];
  const pro = splitMentions('@GLM Pro tolong cek', mirip);
  assert.equal(pro[0].seat.code, 'C02');
  assert.equal(pro[0].text, '@GLM Pro');
  const biasa = splitMentions('@GLM tolong cek', mirip);
  assert.equal(biasa[0].seat.code, 'M02');
  assert.equal(biasa[0].text, '@GLM');
});

test('splitMentions: @ di tengah kata dan nama diikuti huruf bukan mention', () => {
  assert.deepEqual(splitMentions('halo@Plamo', seats), [{ text: 'halo@Plamo' }]);
  assert.deepEqual(splitMentions('@Plamoku beda orang', seats), [{ text: '@Plamoku beda orang' }]);
  assert.deepEqual(splitMentions('kode x@M02y bukan', seats), [{ text: 'kode x@M02y bukan' }]);
});

test('splitMentions: banyak mention berurutan dan tanda baca', () => {
  const segments = splitMentions('@Plamo,@GLM! (@Kopi Susu).', seats);
  assert.deepEqual(segments.map(s => s.text), ['@Plamo', ',', '@GLM', '! (', '@Kopi Susu', ').']);
  assert.deepEqual(segments.filter(s => s.seat).map(s => s.seat.code), ['M01', 'M02', 'C01']);
});
