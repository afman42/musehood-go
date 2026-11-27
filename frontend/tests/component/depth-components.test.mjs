// Component tests untuk UI fitur pendalaman 27 Nov 2026: dialog debat,
// bantuan shortcut, keamanan PIN, log audit tool, reaksi + aksi pesan
// baru (rerun/fork/ekspor), bilah ruang (arsip/badge), editor urutan
// bicara, dan toggle diskusi baru (cache/rantai/kepadatan).
// Interaksi komponen<->API (pasang skill 409, terapkan persona, toggle
// kepadatan) diuji di tests/frontend-e2e-depth.mjs.
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { renderComponent } from './compile.mjs';

const noop = () => {};
const seat = { code: 'M01', name: 'Plamo', role: 'Pelaksana', provider: 'gateway', present: true, avatar: '/avatars/m01.svg' };
const seat2 = { ...seat, code: 'M02', name: 'GLM', role: 'Peninjau' };
const museSeat = { ...seat, code: 'MUSE', name: 'Muse', role: 'Admin', provider: 'muse' };
const baseMessage = {
  id: 'm1', createdAt: '2026-11-27T10:00:00.000Z', authorType: 'seat', seat: 'M01',
  text: 'Halo dunia', status: 'completed',
};
const bubbleProps = {
  seat, seats: [seat, seat2],
  own: false, name: 'Plamo', avatar: '/avatars/m01.svg',
  quoted: undefined, quotedAuthorName: '', editing: false, reactOpen: false,
  emojis: ['👍', '❤️', '😂', '😮', '😢', '🙏', '🔥', '👏'], query: '',
  onReply: noop, onToggleReactPicker: noop, onReact: noop, onPin: noop,
  onEditStart: noop, onEditSave: noop, onEditCancel: noop, onDelete: noop,
  onJumpQuote: noop, onImageLoad: noop, onMentionClick: noop, onRegenerate: noop,
  onRerun: noop, onFork: noop, onExport: noop,
};

test('DebateDialog: dua select hanya kursi gateway, tombol mulai nonaktif', async () => {
  const html = await renderComponent('DebateDialog', {
    seats: [museSeat, seat, seat2], notice: undefined, onStart: async () => {},
  });
  assert.match(html, /id="debateDialog"/);
  assert.match(html, /aria-labelledby="debateTitle"/);
  assert.match(html, /Pihak pro/);
  assert.match(html, /Pihak kontra/);
  // Kursi antrean Muse tidak ikut pilihan debat.
  assert.ok(!html.includes('Muse (MUSE)'), 'kursi muse tidak boleh jadi peserta debat');
  assert.equal((html.match(/Plamo \(M01\)/g) || []).length, 2);
  assert.equal((html.match(/GLM \(M02\)/g) || []).length, 2);
  // Topik kosong + kursi belum dipilih: tombol mulai nonaktif.
  assert.match(html, /<button class="primary-button" type="submit" disabled[^>]*>Mulai Debat<\/button>/);
});

test('DebateDialog: kurang dari dua kursi gateway menampilkan petunjuk', async () => {
  const html = await renderComponent('DebateDialog', {
    seats: [museSeat, seat], notice: undefined, onStart: async () => {},
  });
  assert.match(html, /Debat butuh minimal dua kursi gateway/);
});

test('ShortcutHelpDialog: semua binding terdaftar dengan kbd', async () => {
  const html = await renderComponent('ShortcutHelpDialog', {});
  assert.match(html, /id="shortcutHelpDialog"/);
  for (const keys of ['Ctrl/Cmd + K', 'Ctrl/Cmd + Enter', 'Ctrl/Cmd + /', 'Alt + 1…9', 'Esc', '?']) {
    assert.ok(html.includes(`<kbd>${keys}</kbd>`), `binding ${keys} hilang`);
  }
  assert.match(html, /Fokus pencarian pesan/);
  assert.match(html, /Pindah ruang/);
  assert.match(html, /role="table"/);
});

test('SettingsSecurity: PIN tidak aktif — form pasang PIN saja', async () => {
  const html = await renderComponent('SettingsSecurity', {
    current: { settings: { hasPin: false } }, notify: noop, refresh: async () => {},
  });
  assert.match(html, /PIN tidak aktif/);
  assert.match(html, /Pasang PIN/);
  assert.ok(!html.includes('PIN lama'), 'input PIN lama tidak tampil tanpa PIN');
  assert.ok(!html.includes('Hapus PIN'));
  assert.ok(!html.includes('Keluar (logout)'));
});

test('SettingsSecurity: PIN aktif — ganti/hapus/logout tampil', async () => {
  const html = await renderComponent('SettingsSecurity', {
    current: { settings: { hasPin: true } }, notify: noop, refresh: async () => {},
  });
  assert.match(html, /<strong>PIN aktif<\/strong>/);
  assert.match(html, /PIN lama/);
  assert.match(html, /Ganti PIN/);
  assert.match(html, /Hapus PIN/);
  assert.match(html, /Keluar \(logout\)/);
  assert.match(html, /type="password"/);
});

test('SettingsToolLog: keadaan kosong + tombol segarkan', async () => {
  const html = await renderComponent('SettingsToolLog', { notify: noop });
  assert.match(html, /Log Audit Tool/);
  assert.match(html, /Belum ada pemakaian tool tercatat/);
  assert.match(html, /Segarkan/);
  assert.match(html, /maksimal 500 entri/);
});

test('MessageBubble: pil reaksi dengan hitungan + kelas mine', async () => {
  const html = await renderComponent('MessageBubble', {
    ...bubbleProps,
    message: {
      ...baseMessage,
      reactions: [
        { emoji: '👍', count: 3, byOwner: true },
        { emoji: '🔥', count: 1, byOwner: false },
      ],
    },
  });
  assert.match(html, /reaction-bar/);
  assert.match(html, /class="reaction-pill mine"[^>]*>👍 3<\/button>/);
  assert.match(html, /class="reaction-pill"[^>]*>🔥 1<\/button>/);
});

test('MessageBubble: pemilih reaksi terbuka berisi 8 emoji', async () => {
  const html = await renderComponent('MessageBubble', {
    ...bubbleProps, reactOpen: true, message: baseMessage,
  });
  assert.match(html, /emoji-picker/);
  for (const emoji of bubbleProps.emojis) {
    assert.ok(html.includes(`>${emoji}</button>`), `emoji ${emoji} hilang`);
  }
});

test('MessageBubble: label tahap debat tampil sebagai final-label', async () => {
  const html = await renderComponent('MessageBubble', {
    ...bubbleProps,
    message: { ...baseMessage, label: 'Debat — Pro' },
  });
  assert.match(html, /final-label">Debat — Pro<\/div>/);
  assert.match(html, /message-final/);
});

test('MessageBubble: aksi rerun hanya pemilik, regenerate hanya kursi, fork/ekspor selalu', async () => {
  const own = await renderComponent('MessageBubble', {
    ...bubbleProps, own: true, name: 'Arman',
    message: { ...baseMessage, authorType: 'owner', seat: undefined },
  });
  assert.match(own, /aria-label="Jalankan ulang dari sini"/);
  assert.ok(!own.includes('Buat ulang balasan'));
  assert.match(own, /aria-label="Fork ke ruang baru"/);
  assert.match(own, /aria-label="Ekspor diskusi ini"/);

  const seatHtml = await renderComponent('MessageBubble', { ...bubbleProps, message: baseMessage });
  assert.match(seatHtml, /aria-label="Buat ulang balasan"/);
  assert.ok(!seatHtml.includes('Jalankan ulang dari sini'));
  assert.match(seatHtml, /aria-label="Fork ke ruang baru"/);
});

test('PinnedBar: ringkasan ruang tersemat + potongan 60 karakter', async () => {
  const long = 'Ringkasan diskusi: ' + 'x'.repeat(80);
  const html = await renderComponent('PinnedBar', {
    pinned: [{ ...baseMessage, id: 'sum1', text: long, pinned: true }],
    ownerName: 'Arman', seats: [seat], onJump: noop,
  });
  assert.match(html, /📌 Tersemat/);
  assert.match(html, /Plamo: Ringkasan diskusi:/);
  assert.ok(!html.includes(long), 'teks panjang harus dipotong');
});

test('RoomBar: ruang terarsip tersembunyi, badge belum dibaca, ruang tersemat dulu', async () => {
  const html = await renderComponent('RoomBar', {
    rooms: [
      { id: 'r1', name: 'Utama', createdAt: '', messageCount: 5 },
      { id: 'r2', name: 'Arsipan', createdAt: '', archived: true, messageCount: 9 },
      { id: 'r3', name: 'Ramai', createdAt: '', messageCount: 150 },
      { id: 'r4', name: 'Semat', createdAt: '', pinned: true, messageCount: 3 },
    ],
    activeRoomId: 'r1', onSwitch: noop, onAdd: noop, onSummarize: noop, onTogglePin: noop,
  });
  assert.ok(!html.includes('Arsipan'), 'ruang terarsip tidak tampil di bilah');
  assert.match(html, /room-badge" aria-label="150 belum dibaca">99\+</);
  assert.match(html, /room-badge" aria-label="3 belum dibaca">3</);
  // Ruang aktif tidak diberi badge walau ada pesan.
  assert.ok(!html.includes('aria-label="5 belum dibaca"'));
  // Ruang tersemat tampil sebelum ruang biasa.
  assert.ok(html.indexOf('Semat') < html.indexOf('Utama'), 'ruang tersemat harus pertama');
});

const roomEdit = { id: 'r2', name: 'Kedua', seatCodes: ['M01', 'M02'], seatOrder: ['M02', 'M01'] };
const roomsCurrent = {
  seats: [seat, seat2],
  rooms: [
    { id: 'r1', name: 'Utama', createdAt: '', active: true },
    { id: 'r2', name: 'Kedua', createdAt: '', active: false },
  ],
};

test('SettingsRooms: chip urutan bicara bernomor + tombol arsipkan', async () => {
  const html = await renderComponent('SettingsRooms', {
    roomEdits: [roomEdit], current: roomsCurrent, refresh: async () => {}, notify: noop, onSync: noop,
  });
  assert.match(html, /1\. GLM ✕/);
  assert.match(html, /2\. Plamo ✕/);
  assert.match(html, /Reset urutan/);
  assert.match(html, />Arsipkan<\/button>/);
  assert.match(html, /Simpan ruang/);
});

test('SettingsRooms: ruang terarsip berlabel + tombol pulihkan; urutan kosong = bawaan', async () => {
  const current = {
    seats: [seat, seat2],
    rooms: [
      { id: 'r1', name: 'Utama', createdAt: '', active: true },
      { id: 'r2', name: 'Kedua', createdAt: '', active: false, archived: true },
    ],
  };
  const html = await renderComponent('SettingsRooms', {
    roomEdits: [{ ...roomEdit, seatOrder: [] }], current, refresh: async () => {}, notify: noop, onSync: noop,
  });
  assert.match(html, /Ruang ini terarsip/);
  assert.match(html, />Pulihkan<\/button>/);
  assert.match(html, /Urutan bawaan/);
  assert.match(html, /\+ Plamo/);
});

test('SettingsDiscussion: toggle cache/rantai + pemilih kepadatan', async () => {
  const seatEdits = [{ code: 'M01', name: 'Plamo', role: 'Pelaksana', provider: 'gateway', model: 'plain', prompt: '', fallbackModel: '', fallbackModels: '', disabledTools: [], memory: '', skills: '' }];
  const html = await renderComponent('SettingsDiscussion', {
    seats: seatEdits, responseCacheEnabled: true, chainReplies: true, density: 'compact',
  });
  assert.match(html, /Cache respons/);
  assert.match(html, /Balasan berantai/);
  assert.match(html, /id="densitySelect"/);
  assert.match(html, /<option value="compact"[^>]*>Kompak<\/option>/);
  // Switch yang aktif dirender dengan atribut checked.
  assert.match(html, /id="responseCacheEnabled"[^>]*checked/);
  assert.match(html, /id="chainReplies"[^>]*checked/);
});

test('SeatStrip: peran hasil persona tampil di kartu kursi', async () => {
  const html = await renderComponent('SeatStrip', {
    seats: [{ ...seat, role: 'Peninjau Kritis' }], onMention: noop,
  });
  assert.match(html, /Peninjau Kritis/);
});
