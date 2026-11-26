// Unit tests untuk helper API frontend: label protokol, errorText,
// dismissBackdrop, dan pembungkus fetch api/apiDelete (fetch di-stub).
import assert from 'node:assert/strict';
import { afterEach, test } from 'node:test';
import { api, apiDelete, dismissBackdrop, errorText, protocolLabels } from '../../src/lib/api.ts';

const originalFetch = globalThis.fetch;
afterEach(() => { globalThis.fetch = originalFetch; });

function stubFetch(handler) {
  const calls = [];
  globalThis.fetch = async (route, options) => {
    calls.push({ route, options });
    return handler(route, options);
  };
  return calls;
}

function jsonResponse(ok, status, body) {
  return { ok, status, json: async () => body };
}

test('protocolLabels: kelima protokol terdaftar dengan label', () => {
  assert.deepEqual(Object.keys(protocolLabels).sort(), ['anthropic', 'gemini', 'ollama', 'openai', 'openai-responses']);
  for (const label of Object.values(protocolLabels)) {
    assert.ok(typeof label === 'string' && label.length > 0);
  }
});

test('errorText: Error, string, dan nilai lain', () => {
  assert.equal(errorText(new Error('pesan galat')), 'pesan galat');
  assert.equal(errorText('teks langsung'), 'teks langsung');
  assert.equal(errorText(42), '42');
  assert.equal(errorText(null), 'null');
});

test('api: GET tanpa body memakai cache no-store', async () => {
  const calls = stubFetch(async () => jsonResponse(true, 200, { ok: true }));
  const out = await api('/api/state');
  assert.deepEqual(out, { ok: true });
  assert.equal(calls[0].route, '/api/state');
  assert.deepEqual(calls[0].options, { cache: 'no-store' });
});

test('api: POST JSON mengirim method, body, dan header', async () => {
  const calls = stubFetch(async () => jsonResponse(true, 202, { jobId: 'x' }));
  await api('/api/message', { text: 'halo' });
  assert.equal(calls[0].options.method, 'POST');
  assert.equal(calls[0].options.body, JSON.stringify({ text: 'halo' }));
  assert.equal(calls[0].options.headers['Content-Type'], 'application/json');
});

test('api: FormData dikirim multipart tanpa header JSON', async () => {
  const calls = stubFetch(async () => jsonResponse(true, 200, {}));
  const form = new FormData();
  form.set('image', new Blob(['x']), 'x.png');
  await api('/api/upload-image', form);
  assert.equal(calls[0].options.method, 'POST');
  assert.equal(calls[0].options.body, form);
  assert.equal(calls[0].options.headers, undefined);
});

test('api: respons error server menjadi pesan Error', async () => {
  stubFetch(async () => jsonResponse(false, 400, { error: 'Nama wajib diisi.' }));
  await assert.rejects(() => api('/api/settings', {}), /Nama wajib diisi\./);
});

test('api: error tanpa field error memakai HTTP status', async () => {
  stubFetch(async () => jsonResponse(false, 500, {}));
  await assert.rejects(() => api('/api/state'), /HTTP 500/);
});

test('api: respons bukan JSON ditolak sebagai tidak valid', async () => {
  stubFetch(async () => ({ ok: true, status: 200, json: async () => { throw new Error('bukan json'); } }));
  await assert.rejects(() => api('/api/state'), /Respons server tidak valid\./);
});

test('apiDelete: memakai DELETE + no-store', async () => {
  const calls = stubFetch(async () => jsonResponse(true, 200, { ok: true }));
  await apiDelete('/api/rooms/r1');
  assert.equal(calls[0].options.method, 'DELETE');
  assert.equal(calls[0].options.cache, 'no-store');
});

test('dismissBackdrop: klik di luar dialog menutup, di dalam tidak', () => {
  let closed = 0;
  const dialog = {
    getBoundingClientRect: () => ({ left: 100, right: 300, top: 100, bottom: 300 }),
    close: () => { closed++; },
  };
  // Target bukan dialog (klik konten) -> abaikan.
  dismissBackdrop({ target: {}, clientX: 0, clientY: 0 }, dialog);
  assert.equal(closed, 0);
  // Klik backdrop di dalam rect dialog -> tidak menutup.
  dismissBackdrop({ target: dialog, clientX: 200, clientY: 200 }, dialog);
  assert.equal(closed, 0);
  // Klik backdrop di luar rect -> menutup.
  dismissBackdrop({ target: dialog, clientX: 50, clientY: 200 }, dialog);
  assert.equal(closed, 1);
  dismissBackdrop({ target: dialog, clientX: 200, clientY: 400 }, dialog);
  assert.equal(closed, 2);
});
