// Unit test composable chat.svelte.ts: file dikompilasi dengan
// svelte/compiler (setelah strip tipe TS) lalu dijalankan di Node
// dengan fetch + document yang di-stub.
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
const source = readFileSync(join(libDir, 'chat.svelte.ts'), 'utf8');
const compiled = compileModule(stripTypeScriptTypes(source), { filename: 'chat.svelte.js' }).js.code
  .replace(/from\s+['"]\.\/api['"]/g, "from '../../../src/lib/api.ts'")
  .replace(/from\s+['"]\.\/types['"]/g, "from '../../../src/lib/types.ts'")
  .replace(/from\s+['"]\.\/format['"]/g, "from '../../../src/lib/format.ts'");
writeFileSync(join(tmpDir, 'chat.mjs'), compiled);
const { createChat, seenCount, unreadCount, notificationsEnabled, NOTIFY_KEY } = await import('./.tmp/chat.mjs');

const darkToggles = [];
globalThis.document = {
  hidden: false,
  querySelector: () => null,
  documentElement: { classList: { toggle: (name, on) => darkToggles.push([name, on]) } },
};

const sampleState = () => ({
  owner: { name: 'Arman' },
  group: { name: 'Grup' },
  seats: [
    { code: 'M01', name: 'Plamo', avatar: '' },
    { code: 'M02', name: 'GLM', avatar: '' },
  ],
  rooms: [
    { id: 'r1', name: 'Utama', seatCodes: [] },
    { id: 'r2', name: 'Riset', seatCodes: ['M02'] },
  ],
  activeRoomId: 'r2',
  messages: [
    { id: 'm1', text: 'satu', pinned: true },
    { id: 'm2', text: 'dua' },
  ],
  settings: { baseUrl: 'http://gateway', gatewayProtocol: 'openai', hasApiKey: true, darkMode: true },
});

let calls;
let stateBody;
let failState;
function stubFetch() {
  calls = [];
  stateBody = sampleState();
  failState = false;
  globalThis.fetch = async (url, init = {}) => {
    const body = typeof init.body === 'string' ? JSON.parse(init.body) : init.body;
    calls.push({ url: String(url), method: init.method || 'GET', body });
    if (String(url) === '/api/state') {
      if (failState) throw new Error('jaringan putus');
      return { ok: true, status: 200, json: async () => structuredClone(stateBody) };
    }
    if (String(url).startsWith('/api/search')) {
      return { ok: true, status: 200, json: async () => ({ results: [{ id: 'm1', text: 'satu' }] }) };
    }
    if (String(url) === '/api/upload-image') {
      return { ok: true, status: 200, json: async () => ({ imageUrl: '/images/x.png' }) };
    }
    return { ok: true, status: 200, json: async () => ({}) };
  };
}
const wait = ms => new Promise(r => setTimeout(r, ms));

test('keadaan awal kosong', () => {
  stubFetch();
  const chat = createChat({});
  assert.equal(chat.current, undefined);
  assert.deepEqual(chat.pinnedMessages, []);
  assert.equal(chat.connectionReady, false);
  assert.equal(chat.notice, undefined);
});

test('refresh menerapkan state + derived', async () => {
  stubFetch();
  let before = 0;
  let after = 0;
  const chat = createChat({ beforeApply: () => before++, afterApply: () => after++ });
  await chat.refresh();
  assert.equal(chat.current.seats.length, 2);
  assert.equal(chat.activeRoom.name, 'Riset');
  assert.deepEqual(chat.roomSeats.map(s => s.code), ['M02']); // seatCodes ruang aktif
  assert.deepEqual(chat.pinnedMessages.map(m => m.id), ['m1']);
  assert.equal(chat.connectionReady, true);
  assert.deepEqual(darkToggles.find(([name]) => name === 'dark'), ['dark', true]);
  assert.deepEqual(darkToggles.find(([name]) => name === 'density-compact'), ['density-compact', false]);
  assert.equal(before, 1);
  assert.equal(after, 1);
  // Refresh kedua tanpa perubahan pesan: afterApply tidak dipanggil lagi.
  await chat.refresh();
  assert.equal(before, 2);
  assert.equal(after, 1);
});

test('notify mengisi notice', () => {
  stubFetch();
  const chat = createChat({});
  chat.notify('Halo', true);
  assert.equal(chat.notice.text, 'Halo');
  assert.equal(chat.notice.error, true);
  assert.equal(chat.notice.target, null);
});

test('sendMessage memposting payload lengkap lalu refresh', async () => {
  stubFetch();
  const chat = createChat({});
  await chat.refresh();
  calls = [];
  await chat.sendMessage({ text: 'hai', replyTo: 'm1' });
  assert.equal(calls[0].url, '/api/message');
  assert.equal(calls[0].method, 'POST');
  assert.deepEqual(calls[0].body, { text: 'hai', roomId: 'r2', replyTo: 'm1' });
  assert.equal(calls[1].url, '/api/state');
});

test('aksi pesan memanggil route yang benar', async () => {
  stubFetch();
  const chat = createChat({});
  await chat.togglePin({ id: 'm1' });
  await chat.toggleReact({ id: 'm2' }, '👍');
  await chat.removeMessage({ id: 'm3' });
  await chat.saveEdit('m4', 'baru');
  await chat.decideApproval('a1', true);
  const posts = calls.filter(c => c.method === 'POST').map(c => [c.url, c.body]);
  assert.deepEqual(posts, [
    ['/api/message/m1/pin', {}],
    ['/api/message/m2/react', { emoji: '👍' }],
    ['/api/message/m3/delete', {}],
    ['/api/message/m4/edit', { text: 'baru' }],
    ['/api/approvals/a1', { approved: true }],
  ]);
});

test('uploadImage mengembalikan imageUrl', async () => {
  stubFetch();
  const chat = createChat({});
  const url = await chat.uploadImage(new File(['x'], 'x.png', { type: 'image/png' }));
  assert.equal(url, '/images/x.png');
  assert.ok(calls[0].body instanceof FormData);
});

test('upload dengan callback progress tetap jalan (fallback fetch di Node)', async () => {
  stubFetch();
  const chat = createChat({});
  const seen = [];
  const url = await chat.uploadImage(new File(['x'], 'x.png', { type: 'image/png' }), pct => seen.push(pct));
  assert.equal(url, '/images/x.png');
  // Di Node tidak ada XMLHttpRequest, jadi jalur fetch dipakai dan
  // callback progress tidak dipanggil — hasil unggahan tetap benar.
  assert.deepEqual(seen, []);
});

test('scheduleSearch debounce + clearSearchResults', async () => {
  stubFetch();
  const chat = createChat({});
  chat.scheduleSearch('satu');
  await wait(450);
  assert.deepEqual(chat.serverResults.map(r => r.id), ['m1']);
  assert.ok(calls.some(c => c.url === '/api/search?q=satu'));
  chat.clearSearchResults();
  assert.deepEqual(chat.serverResults, []);
});

test('startPolling berulang lalu berhenti', async () => {
  stubFetch();
  const chat = createChat({});
  const stop = chat.startPolling();
  await wait(1600);
  const during = calls.filter(c => c.url === '/api/state').length;
  assert.ok(during >= 3, `polling hanya ${during}x`);
  stop();
  await wait(900);
  const after = calls.filter(c => c.url === '/api/state').length;
  assert.equal(after, during);
});

test('polling gagal memberi notifikasi koneksi terputus sekali', async () => {
  stubFetch();
  failState = true;
  const chat = createChat({});
  const stop = chat.startPolling();
  await wait(1600);
  stop();
  assert.ok(chat.notice?.text.includes('Koneksi server terputus'), chat.notice?.text);
  assert.equal(chat.notice.error, true);
});

test('polling saat tab tersembunyi memakai interval lambat', async () => {
  stubFetch();
  const chat = createChat({});
  globalThis.document.hidden = true;
  try {
    const stop = chat.startPolling();
    await wait(1600);
    stop();
    const count = calls.filter(c => c.url === '/api/state').length;
    // Interval tersembunyi 3000 ms: dalam 1,6 dtk hanya polling awal.
    assert.equal(count, 1, `polling tersembunyi ${count}x`);
  } finally {
    globalThis.document.hidden = false;
  }
});

function stubNotifyEnv({ permission = 'granted', enabled = true } = {}) {
  const shown = [];
  globalThis.Notification = class {
    static permission = permission;
    constructor(title, opts) { shown.push({ title, opts }); }
  };
  const store = new Map();
  if (enabled) store.set(NOTIFY_KEY, '1');
  globalThis.localStorage = {
    getItem: k => (store.has(k) ? store.get(k) : null),
    setItem: (k, v) => store.set(k, String(v)),
    removeItem: k => store.delete(k),
  };
  return shown;
}

test('maybeNotify: hanya saat tab tersembunyi + izin granted + toggle aktif', async () => {
  stubFetch();
  const shown = stubNotifyEnv();
  const seatDone = (id, text) => ({ id, authorType: 'seat', seat: 'M01', status: 'completed', text });
  stateBody.messages = [seatDone('n1', 'lama')];
  const chat = createChat({});
  await chat.refresh(); // priming: pesan lama tidak dinotifikasi
  assert.equal(shown.length, 0);

  // Tab terlihat: balasan baru tidak dinotifikasi.
  stateBody.messages = [seatDone('n1', 'lama'), seatDone('n2', 'baru terlihat')];
  await chat.refresh();
  assert.equal(shown.length, 0);

  // Tab tersembunyi: balasan berikutnya dinotifikasi sekali.
  globalThis.document.hidden = true;
  try {
    stateBody.messages = [...stateBody.messages, seatDone('n3', 'baru tersembunyi')];
    await chat.refresh();
    assert.equal(shown.length, 1);
    assert.match(shown[0].title, /Plamo membalas/);
    assert.equal(shown[0].opts.body, 'baru tersembunyi');
    // Refresh ulang tanpa pesan baru: tidak dobel.
    await chat.refresh();
    assert.equal(shown.length, 1);
  } finally {
    globalThis.document.hidden = false;
  }

  // Izin ditolak: tidak ada notifikasi walau tersembunyi.
  stubNotifyEnv({ permission: 'denied' });
  globalThis.document.hidden = true;
  try {
    stateBody.messages = [...stateBody.messages, seatDone('n4', 'izin ditolak')];
    await chat.refresh();
    assert.equal(shown.length, 1);
  } finally {
    globalThis.document.hidden = false;
  }
});

test('notificationsEnabled + seenCount/unreadCount membaca localStorage', () => {
  stubNotifyEnv();
  assert.equal(notificationsEnabled(), true);
  stubNotifyEnv({ enabled: false });
  assert.equal(notificationsEnabled(), false);
  stubNotifyEnv({ permission: 'denied' });
  assert.equal(notificationsEnabled(), false);

  const store = new Map([['musehood-seencount-r1', '7']]);
  globalThis.localStorage = {
    getItem: k => (store.has(k) ? store.get(k) : null),
    setItem: (k, v) => store.set(k, String(v)),
    removeItem: k => store.delete(k),
  };
  assert.equal(seenCount('r1'), 7);
  assert.equal(seenCount('rX'), 0);
  assert.equal(unreadCount({ id: 'r1', messageCount: 10 }), 3);
  assert.equal(unreadCount({ id: 'r1', messageCount: 5 }), 0);
  assert.equal(unreadCount({ id: 'r1' }), 0);
});

test('PIN: state 401 -> pinRequired, login membuka, logout menutup', async () => {
  const localCalls = [];
  let pinActive = true;
  globalThis.fetch = async (url, init = {}) => {
    const body = typeof init.body === 'string' ? JSON.parse(init.body) : init.body;
    localCalls.push({ url: String(url), method: init.method || 'GET', body });
    if (String(url) === '/api/state') {
      if (pinActive) return { ok: false, status: 401, json: async () => ({ error: 'PIN diperlukan' }) };
      return { ok: true, status: 200, json: async () => structuredClone(sampleState()) };
    }
    if (String(url) === '/api/login') {
      if (body.pin !== '1234') return { ok: false, status: 401, json: async () => ({ error: 'PIN salah' }) };
      pinActive = false;
      return { ok: true, status: 200, json: async () => ({}) };
    }
    if (String(url) === '/api/logout') {
      pinActive = true;
      return { ok: true, status: 200, json: async () => ({}) };
    }
    return { ok: true, status: 200, json: async () => ({}) };
  };
  const chat = createChat({});
  await chat.refresh();
  assert.equal(chat.pinRequired, true);
  assert.equal(chat.current, undefined);
  await assert.rejects(() => chat.login('0000'), /PIN salah/);
  assert.equal(chat.pinRequired, true);
  await chat.login('1234');
  assert.equal(chat.pinRequired, false);
  assert.equal(chat.current.seats.length, 2);
  await chat.logout();
  assert.equal(chat.current, undefined);
  assert.equal(chat.pinRequired, true);
  assert.ok(localCalls.some(c => c.url === '/api/login' && c.body.pin === '1234'));
});

test('aksi debat/rerun/fork/regenerate memanggil route yang benar', async () => {
  stubFetch();
  const chat = createChat({});
  await chat.refresh();
  calls = [];
  await chat.startDebate('M01', 'M02', 'Kopi vs teh');
  await chat.rerunMessage({ id: 'm9' });
  await chat.forkMessage({ id: 'm9' });
  await chat.regenerateMessage({ id: 'm8' });
  const posts = calls.filter(c => c.method === 'POST').map(c => [c.url, c.body]);
  assert.deepEqual(posts, [
    ['/api/debate', { seatA: 'M01', seatB: 'M02', topic: 'Kopi vs teh', roomId: 'r2' }],
    ['/api/message/m9/rerun', {}],
    ['/api/message/m9/fork', {}],
    ['/api/message/m8/regenerate', {}],
  ]);
});
