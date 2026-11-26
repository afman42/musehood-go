// Unit tests untuk helper format frontend (dijalankan Node --test dengan
// type-stripping bawaan Node 24, tanpa build step).
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { attachmentLabel, humanSize, isImageAttachment, splitMentions } from '../../src/lib/format.ts';

test('humanSize: batas byte/KB/MB', () => {
  assert.equal(humanSize(0), '0 B');
  assert.equal(humanSize(512), '512 B');
  assert.equal(humanSize(1023), '1023 B');
  assert.equal(humanSize(1024), '1.0 KB');
  assert.equal(humanSize(1536), '1.5 KB');
  assert.equal(humanSize(1024 * 1024 - 1), '1024.0 KB');
  assert.equal(humanSize(1024 * 1024), '1.0 MB');
  assert.equal(humanSize(5.5 * 1024 * 1024), '5.5 MB');
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
