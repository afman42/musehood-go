// Test live e2e Musehood Go: menjalankan binary asli pada port uji dengan
// direktori data sementara + mock gateway, lalu menguji semua fitur lewat
// HTTP sungguhan. Juga melakukan cek read-only terhadap server produksi
// lokal di 127.0.0.1:3001 (tanpa mengubah data Arman).
//
// Jalankan: node tests/live-e2e.mjs
// Keluar dengan kode 1 bila ada pemeriksaan yang gagal.

import { spawn } from 'node:child_process';
import { mkdtempSync, writeFileSync, existsSync, mkdirSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { mockGateway, fixtureKey, pixel } from './mock-gateway.mjs';

const repo = dirname(dirname(fileURLToPath(import.meta.url)));
const binary = join(repo, 'musehood-go');
const APP_PORT = 3198;
const MOCK_PORT = 3199;
const APP = `http://127.0.0.1:${APP_PORT}`;
const PROD = 'http://127.0.0.1:3001';

let passed = 0;
let failed = 0;
const failures = [];

function check(name, ok, detail = '') {
  if (ok) {
    passed++;
    console.log(`PASS  ${name}`);
  } else {
    failed++;
    failures.push(name);
    console.log(`FAIL  ${name}${detail ? ' — ' + detail : ''}`);
  }
}

async function api(base, method, path, body, expectStatus) {
  const res = await fetch(base + path, {
    method,
    headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  const text = await res.text();
  let json;
  try { json = JSON.parse(text); } catch { json = undefined; }
  if (expectStatus !== undefined && res.status !== expectStatus) {
    throw new Error(`${method} ${path} -> ${res.status} (mau ${expectStatus}): ${text.slice(0, 200)}`);
  }
  return { status: res.status, json, text, headers: res.headers };
}

async function waitFor(fn, timeoutMs = 15000) {
  const deadline = Date.now() + timeoutMs;
  let last;
  while (Date.now() < deadline) {
    last = await fn();
    if (last) return last;
    await new Promise(r => setTimeout(r, 120));
  }
  return undefined;
}

const state = () => api(APP, 'GET', '/api/state', undefined, 200).then(r => r.json);
const messages = s => s.messages || [];

async function finish(count) {
  const s = await waitFor(async () => {
    const st = await state();
    if (messages(st).length !== count) return undefined;
    if ((st.jobs || []).some(j => j.status !== 'completed')) return undefined;
    return st;
  });
  if (!s) throw new Error(`finish(${count}) timeout`);
  return messages(s);
}

async function pending(kind) {
  const q = await waitFor(async () => {
    const r = await api(APP, 'GET', '/api/queue/muse', undefined, 200);
    return (r.json.requests || []).find(x => x.kind === kind);
  });
  if (!q) throw new Error(`antrean ${kind} tidak ditemukan`);
  return q;
}

async function main() {
  if (!existsSync(binary)) throw new Error(`binary tidak ada: ${binary} (jalankan ./build.sh dulu)`);
  const dataDir = mkdtempSync(join(tmpdir(), 'musehood-live-e2e-'));
  const mock = await mockGateway(MOCK_PORT);
  const mockUrl = mock.url;
  const app = spawn(binary, [], {
    cwd: dataDir,
    env: { ...process.env, PORT: String(APP_PORT), DATA_DIR: dataDir, NO_PROXY: 'localhost,127.0.0.1', no_proxy: 'localhost,127.0.0.1' },
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  let appLog = '';
  app.stdout.on('data', d => { appLog += d; });
  app.stderr.on('data', d => { appLog += d; });
  try {
    const ready = await waitFor(async () => {
      try { const r = await fetch(`${APP}/api/state`); return r.ok; } catch { return false; }
    }, 20000);
    check('binary uji hidup dan melayani /api/state', !!ready, appLog.slice(0, 300));
    if (!ready) return;

    // --- Halaman & state ---
    {
      const r = await api(APP, 'GET', '/', undefined, 200);
      check('GET / melayani HTML aplikasi', r.text.includes('<') && r.headers.get('content-type')?.includes('text/html'));
      const s = await state();
      check('state: 7 kursi bawaan + pemilik Arman', s.seats?.length === 7 && s.owner?.name === 'Arman');
      const spa = await api(APP, 'GET', '/rute/spa/acak', undefined, 200);
      check('fallback SPA 200', spa.status === 200);
      const av = await api(APP, 'GET', '/avatars/muse.svg', undefined, 200);
      check('avatar bawaan SVG dilayani', av.text.includes('<svg'));
    }

    // --- Konfigurasi gateway ke mock ---
    await api(APP, 'POST', '/api/settings', {
      settings: { baseUrl: mockUrl, apiKey: fixtureKey, gatewayProtocol: 'openai' },
      seats: [
        { code: 'M10', model: 'sse' }, { code: 'M01', model: 'array' }, { code: 'M02', model: 'plain' },
        { code: 'M03', model: 'image' }, { code: 'M04', model: 'delta' }, { code: 'M05', model: 'delta' },
      ],
    }, 200);
    {
      const s = await state();
      check('settings: key termask, hasApiKey true', s.settings.hasApiKey === true && !JSON.stringify(s).includes(fixtureKey));
      const conn = await api(APP, 'POST', '/api/test-connection', {}, 200);
      check('Tes Koneksi: daftar model mock', Array.isArray(conn.json.models) && conn.json.models.includes('sse'));
      const tm = await api(APP, 'POST', '/api/test-model', { model: 'sse' }, 200);
      check('Tes Model sse ok', tm.json.ok === true && typeof tm.json.reply === 'string');
      const denied = await api(APP, 'POST', '/api/test-model', { model: 'denied' }, 401);
      check('Tes Model denied: error 401 diteruskan', denied.status === 401);
    }

    // --- Pengaturan & kustomisasi kursi ---
    {
      await api(APP, 'POST', '/api/settings', { settings: { reviewLevel: 'Ketat', darkMode: true } }, 200);
      let s = await state();
      check('pengaturan reviewLevel + darkMode tersimpan', s.settings.reviewLevel === 'Ketat' && s.settings.darkMode === true);
      await api(APP, 'POST', '/api/settings', { settings: { reviewLevel: 'Sedang', darkMode: false } }, 200);
      await api(APP, 'POST', '/api/settings', {
        seats: [{ code: 'MUSE', role: 'Koordinator Uji', provider: 'gateway', model: 'sse' }],
        settings: { integratorSeat: 'M10' },
      }, 200);
      s = await state();
      const muse = s.seats.find(x => x.code === 'MUSE');
      check('kursi MUSE bisa diganti peran/provider/model', muse.role === 'Koordinator Uji' && muse.provider === 'gateway' && muse.model === 'sse');
      check('integratorSeat bisa diganti', s.settings.integratorSeat === 'M10');
      await api(APP, 'POST', '/api/settings', {
        seats: [{ code: 'MUSE', role: 'Admin dan Integrasi', provider: 'muse', model: '' }],
        settings: { integratorSeat: 'MUSE' },
      }, 200);
    }

    // --- CRUD provider ---
    {
      const created = await api(APP, 'POST', '/api/providers', { name: 'Kedua', baseUrl: mockUrl, apiKey: fixtureKey, protocol: 'openai', alias: 'kd' }, 201);
      const id = created.json.id;
      check('provider dibuat dengan alias, tanpa key bocor', created.json.alias === 'kd' && !JSON.stringify(created.json).includes(fixtureKey));
      const models = await api(APP, 'GET', `/api/providers/${id}/models`, undefined, 200);
      check('Fetch Models provider', Array.isArray(models.json.models) && models.json.models.length > 0);
      const tested = await api(APP, 'POST', `/api/providers/${id}/test-models`, { models: ['sse', 'denied'] }, 200);
      check('Tes Models per model (1 ok, 1 gagal)', tested.json.results?.[0]?.ok === true && tested.json.results?.[1]?.ok === false);
      await api(APP, 'POST', `/api/providers/${id}/activate`, {}, 200);
      let s = await state();
      check('provider diaktifkan menyinkronkan settings', s.settings.providerName === 'Kedua');
      await api(APP, 'DELETE', `/api/providers/${id}`, undefined, 200);
      s = await state();
      check('hapus provider aktif mengaktifkan provider lain', s.settings.providerName !== 'Kedua');
    }

    // --- Chat bergilir + antrean Muse ---
    {
      await api(APP, 'POST', '/api/message', { text: 'halo semua' }, 202);
      const q = await pending('chat');
      await api(APP, 'POST', `/api/queue/muse/${q.id}`, { text: 'Halo dari Muse' }, 200);
      const m = await finish(8);
      const order = m.slice(1).map(x => x.seat).join(',');
      check('diskusi bergilir: semua kursi menjawab berurutan', order === 'MUSE,M10,M01,M02,M03,M04,M05', order);
    }

    // --- Mention ---
    {
      await api(APP, 'POST', '/api/message', { text: '@Plamo kerjakan ini' }, 202);
      const m = await finish(10);
      check('mention @Plamo dijawab M01', m[9].seat === 'M01');
    }

    // --- Pipeline Tugas + progres ---
    {
      await api(APP, 'POST', '/api/message', { text: 'Tugas: Buat rencana piknik' }, 202);
      const q = await pending('summary');
      const respDir = join(dataDir, 'queue', 'responses');
      mkdirSync(respDir, { recursive: true });
      writeFileSync(join(respDir, `${q.id}.json`), JSON.stringify({ text: 'Hasil piknik' }));
      const m = await finish(17);
      const order = m.slice(11).map(x => x.seat).join(',');
      check('pipeline Tugas 6 tahap berurutan', order === 'M10,M01,M04,M03,M02,MUSE', order);
      check('Hasil Akhir - Integrasi dari pekerja', m[16].label === 'Hasil Akhir - Integrasi' && m[16].text === 'Hasil piknik');
      check('tahap Visual menghasilkan gambar', typeof m[14].imageUrl === 'string' && m[14].imageUrl.startsWith('/images/'));
      await api(APP, 'POST', '/api/progress', {}, 202);
      const pq = await pending('progress');
      await api(APP, 'POST', `/api/queue/muse/${pq.id}`, { text: 'Selesai semua' }, 200);
      const m2 = await finish(19);
      check('Tanya Progres dijawab integrator', m2[18].text === 'Selesai semua');
    }

    // --- Gambar: ---
    {
      await api(APP, 'POST', '/api/message', { text: 'Gambar: kucing di taman' }, 202);
      const m = await finish(21);
      const url = m[20].imageUrl;
      check('Gambar: menghasilkan imageUrl', typeof url === 'string' && url.startsWith('/images/'), JSON.stringify(m[20]).slice(0, 200));
      if (url) {
        const img = await fetch(APP + url);
        check('file gambar bisa diambil (200)', img.ok && (await img.arrayBuffer()).byteLength > 0);
      }
    }

    // --- Operasi pesan ---
    {
      await api(APP, 'POST', '/api/settings', { settings: { roundRobin: false } }, 200);
      await api(APP, 'POST', '/api/message', { text: 'pesan untuk diedit' }, 202);
      const q = await pending('chat');
      await api(APP, 'POST', `/api/queue/muse/${q.id}`, { text: 'balasan kursi' }, 200);
      const m = await finish(23);
      const ownerID = m[21].id, seatID = m[22].id;
      await api(APP, 'POST', `/api/message/${ownerID}/edit`, { text: 'sudah diedit' }, 200);
      check('edit pesan pemilik ok', true);
      await api(APP, 'POST', `/api/message/${seatID}/edit`, { text: 'tidak boleh' }, 403);
      check('edit pesan kursi ditolak 403', true);
      await api(APP, 'POST', `/api/message/${ownerID}/react`, { emoji: '👍' }, 200);
      check('reaksi emoji diizinkan ok', true);
      await api(APP, 'POST', `/api/message/${ownerID}/react`, { emoji: '💥' }, 400);
      check('reaksi emoji di luar daftar ditolak 400', true);
      await api(APP, 'POST', '/api/message', { text: 'balasan', replyTo: ownerID }, 202);
      const q2 = await pending('chat');
      await api(APP, 'POST', `/api/queue/muse/${q2.id}`, { text: 'balasan kedua' }, 200);
      const m2 = await finish(25);
      check('balasan menyimpan replyTo', m2[23].replyTo === ownerID);
      await api(APP, 'POST', `/api/message/${ownerID}/delete`, {}, 200);
      const s = await state();
      check('hapus pesan pemilik ok', !messages(s).some(x => x.id === ownerID));
      await api(APP, 'POST', '/api/settings', { settings: { roundRobin: true } }, 200);
    }

    // --- Unggahan ---
    {
      const form = new FormData();
      form.append('target', 'owner');
      form.append('avatar', new Blob([pixel], { type: 'image/png' }), 'a.png');
      const av = await fetch(`${APP}/api/upload-avatar`, { method: 'POST', body: form });
      const avJson = await av.json().catch(() => ({}));
      check('unggah avatar ok', av.ok && String(avJson.avatar || '').startsWith('/avatars/'), JSON.stringify(avJson).slice(0, 150));

      const form2 = new FormData();
      form2.append('image', new Blob([pixel], { type: 'image/png' }), 'g.png');
      const im = await fetch(`${APP}/api/upload-image`, { method: 'POST', body: form2 });
      const imJson = await im.json().catch(() => ({}));
      check('unggah gambar ok', im.ok && String(imJson.imageUrl || '').startsWith('/images/'), JSON.stringify(imJson).slice(0, 150));

      const form3 = new FormData();
      form3.append('file', new Blob(['isi dokumen'], { type: 'text/plain' }), 'dokumen.txt');
      const fl = await fetch(`${APP}/api/upload-file`, { method: 'POST', body: form3 });
      const flJson = await fl.json().catch(() => ({}));
      check('unggah file ok', fl.ok && String(flJson.url || '').startsWith('/uploads/'), JSON.stringify(flJson).slice(0, 150));
      if (flJson.url) {
        const got = await fetch(APP + flJson.url);
        check('file unggahan bisa diambil kembali', got.ok && (await got.text()) === 'isi dokumen');
      }
    }

    // --- Kursi kustom ---
    {
      const created = await api(APP, 'POST', '/api/seats', { name: 'Uji', role: 'Penguji', provider: 'gateway', model: 'sse' }, 201);
      const code = created.json.code;
      check('kursi kustom dibuat', typeof code === 'string' && code.length > 0);
      await api(APP, 'DELETE', '/api/seats/MUSE', undefined, 400);
      check('kursi bawaan tidak bisa dihapus (400)', true);
      await api(APP, 'DELETE', `/api/seats/${code}`, undefined, 200);
      const s = await state();
      check('kursi kustom dihapus', !s.seats.some(x => x.code === code));
    }

    // --- Ekspor ---
    {
      const j = await api(APP, 'GET', '/api/export?format=json', undefined, 200);
      check('ekspor JSON tanpa API key', !j.text.includes(fixtureKey));
      const md = await api(APP, 'GET', '/api/export?format=md', undefined, 200);
      check('ekspor Markdown memuat label integrasi + pemilik', md.text.includes('Hasil Akhir - Integrasi') && md.text.includes('Arman'));
      await api(APP, 'GET', '/api/export?format=pdf', undefined, 400);
      check('ekspor format lain ditolak 400', true);
    }

    // --- Keamanan ---
    {
      // fetch() mengabaikan header Host; pakai http mentah agar Host asing benar-benar terkirim.
      const { request } = await import('node:http');
      const badStatus = await new Promise((resolve, reject) => {
        const req = request({ host: '127.0.0.1', port: APP_PORT, path: '/api/state', method: 'GET', headers: { Host: 'jahat.example' } }, res => { res.resume(); resolve(res.statusCode); });
        req.on('error', reject);
        req.end();
      });
      check('Host asing ditolak 403', badStatus === 403, `status ${badStatus}`);
      const badJson = await fetch(`${APP}/api/message`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{rusak' });
      check('JSON rusak ditolak 400', badJson.status === 400, `status ${badJson.status}`);
      const put = await fetch(`${APP}/api/state`, { method: 'PUT' });
      check('metode salah ditolak 405', put.status === 405, `status ${put.status}`);
    }

    // --- Bersihkan chat ---
    {
      await api(APP, 'POST', '/api/clear', {}, 200);
      const s = await state();
      check('bersihkan chat: pesan kosong, konfigurasi utuh', messages(s).length === 0 && s.seats.length === 7 && s.settings.hasApiKey === true);
    }

    // --- Cek read-only server produksi lokal ---
    {
      try {
        const s = await api(PROD, 'GET', '/api/state', undefined, 200);
        check('produksi :3001 state 200', s.json.seats?.length >= 7);
        check('produksi :3001 tidak membocorkan key mentah', !/sk-[A-Za-z0-9]{8,}/.test(s.text));
        const home = await api(PROD, 'GET', '/', undefined, 200);
        check('produksi :3001 halaman utama 200', home.status === 200);
        const ex = await api(PROD, 'GET', '/api/export?format=json', undefined, 200);
        check('produksi :3001 ekspor JSON 200', ex.status === 200);
      } catch (e) {
        check('produksi :3001 bisa dijangkau', false, String(e).slice(0, 200));
      }
    }
  } finally {
    app.kill('SIGTERM');
    mock.server?.close();
  }
  console.log(`\n=== LIVE E2E: ${passed} PASS, ${failed} FAIL ===`);
  if (failed > 0) {
    console.log('Gagal:', failures.join('; '));
    process.exit(1);
  }
  process.exit(0);
}

main().catch(e => { console.error('ERROR:', e); process.exit(1); });
