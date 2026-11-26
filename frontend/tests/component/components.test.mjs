// Component tests: render SSR komponen Svelte dan periksa markup hasil
// (teks, atribut aksesibilitas, chip tool, badge, status).
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { renderComponent } from './compile.mjs';

const noop = () => {};
const seat = { code: 'M01', name: 'Plamo', role: 'Pelaksana', provider: 'gateway', present: true, avatar: '/avatars/m01.svg' };
const baseMessage = {
  id: 'm1', createdAt: '2026-11-26T10:00:00.000Z', authorType: 'seat', seat: 'M01',
  text: 'Halo dunia', status: 'completed',
};
const bubbleProps = {
  seat, own: false, name: 'Plamo', avatar: '/avatars/m01.svg',
  quoted: undefined, quotedAuthorName: '', editing: false, reactOpen: false,
  emojis: ['👍'], query: '',
  onReply: noop, onToggleReactPicker: noop, onReact: noop, onPin: noop,
  onEditStart: noop, onEditSave: noop, onEditCancel: noop, onDelete: noop,
  onJumpQuote: noop, onImageLoad: noop,
};

test('MessageBubble: teks, nama penulis, dan waktu tampil', async () => {
  const html = await renderComponent('MessageBubble', { ...bubbleProps, message: baseMessage });
  assert.match(html, /Halo dunia/);
  assert.match(html, /Plamo/);
  assert.match(html, /author-name/);
});

test('MessageBubble: chip tool dan badge jawaban terbaik', async () => {
  const html = await renderComponent('MessageBubble', {
    ...bubbleProps,
    message: { ...baseMessage, toolsUsed: ['cuaca', 'run_code'], bestAnswer: true, votes: 2 },
  });
  assert.match(html, /tool-chip/);
  assert.match(html, /cuaca/);
  assert.match(html, /run_code/);
  assert.match(html, /best/i);
});

test('MessageBubble: status mengetik menampilkan indikator', async () => {
  const html = await renderComponent('MessageBubble', {
    ...bubbleProps,
    message: { ...baseMessage, text: '', status: 'typing', statusText: 'Plamo sedang mengetik...' },
  });
  assert.match(html, /typing-dots|sedang mengetik/);
});

test('MessageBubble: teks pemilik tidak di-render sebagai HTML mentah', async () => {
  const html = await renderComponent('MessageBubble', {
    ...bubbleProps, own: true, name: 'Arman',
    message: { ...baseMessage, authorType: 'owner', seat: undefined, text: '<img src=x onerror=alert(1)>' },
  });
  assert.ok(!html.includes('<img src=x'), 'tag img mentah tidak boleh lolos');
  assert.match(html, /&lt;img/);
});

test('SeatStrip: kartu kursi dengan nama dan peran', async () => {
  const html = await renderComponent('SeatStrip', {
    seats: [seat, { ...seat, code: 'M02', name: 'GLM', role: 'Peninjau' }],
    onMention: noop,
  });
  assert.match(html, /Plamo/);
  assert.match(html, /GLM/);
  assert.match(html, /Pelaksana/);
  assert.match(html, /seat-card/);
});

test('ApprovalBanner: judul per kind dan tombol Setujui/Tolak', async () => {
  const approvals = [
    { id: 'a1', seatCode: 'M01', seatName: 'Plamo', kind: 'code', language: 'python', detail: 'print(1)', createdAt: '' },
    { id: 'a2', seatCode: 'M01', seatName: 'Plamo', kind: 'shell', detail: 'ls -la', createdAt: '' },
    { id: 'a3', seatCode: 'M01', seatName: 'Plamo', kind: 'delete', detail: 'file.txt', createdAt: '' },
  ];
  const html = await renderComponent('ApprovalBanner', { approvals, onDecide: noop });
  assert.match(html, /Setujui/);
  assert.match(html, /Tolak/);
  assert.match(html, /print\(1\)/);
  assert.match(html, /ls -la/);
  assert.match(html, /file\.txt/);
});

test('ApprovalBanner: kosong bila tidak ada persetujuan', async () => {
  const html = await renderComponent('ApprovalBanner', { approvals: [], onDecide: noop });
  assert.ok(!html.includes('Setujui'));
});

test('Toast: tampil hanya untuk target yang cocok', async () => {
  const shown = await renderComponent('Toast', { notice: { text: 'Tersimpan', error: false, target: 'page' }, target: 'page' });
  assert.match(shown, /Tersimpan/);
  assert.match(shown, /role="status"/);
  const hidden = await renderComponent('Toast', { notice: { text: 'Tersimpan', error: false, target: 'settings' }, target: 'page' });
  assert.match(hidden, /hidden/);
  assert.ok(!hidden.includes('Tersimpan'));
});

test('RoomBar: ruang aktif ditandai dan tombol tambah ada', async () => {
  const html = await renderComponent('RoomBar', {
    rooms: [{ id: 'room-utama', name: 'Utama', createdAt: '' }, { id: 'r2', name: 'Kedua', createdAt: '' }],
    activeRoomId: 'r2', onSwitch: noop, onAdd: noop,
  });
  assert.match(html, /Utama/);
  assert.match(html, /Kedua/);
  assert.match(html, /active/);
});

test('PinnedBar: daftar pesan terpin dengan label lampiran', async () => {
  const html = await renderComponent('PinnedBar', {
    pinned: [{ ...baseMessage, pinned: true }, { ...baseMessage, id: 'm2', text: '', imageUrl: '/images/x.png' }],
    ownerName: 'Arman', seats: [seat], onJump: noop,
  });
  assert.match(html, /Halo dunia/);
  assert.match(html, /\[gambar\]/);
});

test('SearchBar: jumlah hasil dan navigasi tampil', async () => {
  const html = await renderComponent('SearchBar', {
    query: 'halo', matchCount: 3, matchIndex: 0, serverResults: [],
    onInput: noop, onPrev: noop, onNext: noop, onClose: noop, onJumpResult: noop,
  });
  assert.match(html, /3/);
  assert.match(html, /aria-label/);
});

const composerProps = {
  text: '', submitting: false, asking: false, voting: false,
  uploadingImage: false, uploadingFile: false, uploadProgress: null,
  replyTo: undefined, replyAuthorName: '', pendingImage: '', pendingFile: null,
  onSend: noop, onProgress: noop, onVote: noop, onOpenMention: noop,
  onImagePrompt: noop, onUploadImage: noop, onUploadFile: noop,
  onCancelReply: noop, onCancelImage: noop, onCancelFile: noop,
};

test('Composer: textarea pesan dan tombol aksi berlabel', async () => {
  const html = await renderComponent('Composer', composerProps);
  assert.match(html, /<textarea/);
  assert.match(html, /aria-label/);
  assert.match(html, /Kirim|@ Kursi/);
});

test('Composer: saat mengirim tombol menampilkan status + detik', async () => {
  const html = await renderComponent('Composer', { ...composerProps, submitting: true });
  assert.match(html, /Mengirim… 0 dtk/);
  assert.match(html, /sending/);
});

test('Composer: unggahan menampilkan progress bar + persen', async () => {
  const html = await renderComponent('Composer', { ...composerProps, uploadingFile: true, uploadProgress: 45 });
  assert.match(html, /role="progressbar"/);
  assert.match(html, /aria-valuenow="45"/);
  assert.match(html, /Mengunggah file… 45%/);
});

test('Composer: lampiran selesai bertanda siap dikirim', async () => {
  const html = await renderComponent('Composer', {
    ...composerProps,
    pendingImage: '/images/x.png',
    pendingFile: { name: 'a.txt', size: 12 },
  });
  assert.match(html, /✓ Gambar siap dikirim/);
  assert.match(html, /✓ siap dikirim/);
});

test('AvatarButton: tombol dengan label dan gambar avatar', async () => {
  const html = await renderComponent('AvatarButton', {
    target: 'owner', src: '/avatars/owner.svg', label: 'Ganti foto pemilik', onChoose: noop,
  });
  assert.match(html, /<button/);
  assert.match(html, /Ganti foto pemilik/);
  assert.match(html, /owner\.svg/);
});
