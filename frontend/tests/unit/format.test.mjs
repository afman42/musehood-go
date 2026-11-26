// Unit tests untuk helper format frontend (dijalankan Node --test dengan
// type-stripping bawaan Node 24, tanpa build step).
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { attachmentLabel, humanSize, isImageAttachment } from '../../src/lib/format.ts';

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
