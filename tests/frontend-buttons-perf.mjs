// Performa tombol: tema instan, indikator loading dialog malas,
// prefetch chunk Pengaturan, rating optimistis, indikator ganti ruang.
// Latar: keluhan pemilik "tekan tombol theme dan tombol pengaturan lama".
// Jalankan: node tests/frontend-buttons-perf.mjs
import { spawn } from 'node:child_process';
import { once } from 'node:events';
import { mkdtempSync, readFileSync, writeFileSync, rmSync, mkdirSync } from 'node:fs';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { randomUUID } from 'node:crypto';
import { makeReporter, startGateway, configureMockProvider } from './frontend-harness.mjs';
import { fixtureKey } from './mock-gateway.mjs';

const repo = dirname(dirname(fileURLToPath(import.meta.url)));
const BINARY = join(repo, 'musehood-go');
const report = makeReporter('FRONTEND BUTTONS PERF');

async function freePort() {
  const s = createServer().listen(0, '127.0.0.1');
  await once(s, 'listening');
  const p = s.address().port;
  await new Promise(r => s.close(r));
  return p;
}

async function boot(dir, port) {
  const home = join(dir, 'home');
  mkdirSync(home, { recursive: true });
  const child = spawn(BINARY, [], {
    cwd: dir,
    env: { ...process.env, DATA_DIR: join(dir, 'runtime'), PORT: String(port), HOME: home },
  });
  const url = `http://127.0.0.1:${port}`;
  const deadline = Date.now() + 15000;
  while (Date.now() < deadline) {
    try { const r = await fetch(url + '/api/state'); if (r.ok) break; } catch { /* belum siap */ }
    await new Promise(r => setTimeout(r, 100));
  }
  return { child, url };
}

const dir = mkdtempSync(join(tmpdir(), 'musehood-buttons-'));
const port = await freePort();
let { child, url } = await boot(dir, port);
await fetch(url + '/api/settings', {
  method: 'POST', headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ settings: { darkMode: false } }),
});
child.kill('SIGTERM'); await once(child, 'exit').catch(() => {});

// Seed 300 pesan agar biaya render ulang terasa.
const dataPath = join(dir, 'runtime', 'data.json');
const data = JSON.parse(readFileSync(dataPath, 'utf8'));
data.settings.integratorSeat = 'M10';
data.rooms[0].seatCodes = ['M01', 'M02', 'M04', 'M10'];
const base = Date.parse('2026-11-27T00:00:00Z');
data.messages = [];
for (let i = 0; i < 300; i++) {
  const seat = i % 2 === 1;
  data.messages.push({
    id: randomUUID(), createdAt: new Date(base + i * 1000).toISOString(),
    authorType: seat ? 'seat' : 'owner', ...(seat ? { seat: 'M01' } : {}),
    jobId: randomUUID(), text: 'pesan uji ' + i + ' dengan **markdown** dan teks cukup panjang untuk dirender.',
    status: 'completed', roomId: 'room-utama', branchId: 'main',
  });
}
writeFileSync(dataPath, JSON.stringify(data));
({ child, url } = await boot(dir, port));

const gateway = await startGateway();
await configureMockProvider(url, gateway.url, { M01: 'plain', M02: 'sse', M10: 'array', M04: 'toolcode' });
await fetch(url + '/api/settings', {
  method: 'POST', headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ settings: { integratorSeat: 'M10' } }),
});

const { chromium } = await import('../frontend/node_modules/playwright/index.mjs');
const browser = await chromium.launch();

try {
  // ---------- 1) Tema instan dengan 300 pesan ----------
  {
    const page = await (await browser.newContext()).newPage();
    page.setDefaultTimeout(20000);
    await page.goto(url);
    await page.waitForFunction(() => document.querySelectorAll('.message').length >= 300, null, { timeout: 30000 });
    await page.evaluate(() => {
      window.__longtasks = [];
      new PerformanceObserver(list => {
        for (const e of list.getEntries()) window.__longtasks.push(Math.round(e.duration));
      }).observe({ entryTypes: ['longtask'] });
    });
    const themeMs = await page.evaluate(async () => {
      const t0 = performance.now();
      document.querySelector('#darkToggle').click();
      await new Promise(res => {
        const check = () => { if (document.documentElement.classList.contains('dark')) res(); else requestAnimationFrame(check); };
        check(); setTimeout(res, 5000);
      });
      return Math.round(performance.now() - t0);
    });
    await page.waitForTimeout(800);
    const longtasks = await page.evaluate(() => window.__longtasks);
    report.check(`tema berubah cepat (${themeMs} ms < 300 ms)`, themeMs < 300, `${themeMs} ms`);
    report.check(`tanpa longtask >200 ms saat ganti tema (maks ${Math.max(0, ...longtasks)} ms)`, longtasks.every(t => t <= 200), JSON.stringify(longtasks));

    // ---------- 2) Rating optimistis ----------
    const composer = page.getByRole('textbox', { name: 'Pesan untuk grup' });
    await composer.fill('@Plamo halo tes tombol');
    await composer.press('Enter');
    await page.waitForFunction(() => [...document.querySelectorAll('.message:not(.message-owner)')].some(m => m.textContent.includes('Balasan polos')), null, { timeout: 20000 });
    const ratingMs = await page.evaluate(async () => {
      const msg = [...document.querySelectorAll('.message:not(.message-owner)')].find(m => m.textContent.includes('Balasan polos'));
      msg.scrollIntoView();
      msg.dispatchEvent(new MouseEvent('mouseover', { bubbles: true }));
      const t0 = performance.now();
      msg.querySelector('button[aria-label="Balasan bagus"]').click();
      await new Promise(res => {
        const check = () => { if (msg.querySelector('.rating-button.active')) res(); else requestAnimationFrame(check); };
        check(); setTimeout(res, 5000);
      });
      return Math.round(performance.now() - t0);
    });
    report.check(`rating optimistis tampil cepat (${ratingMs} ms < 300 ms)`, ratingMs < 300, `${ratingMs} ms`);
    await page.context().close();
  }

  // ---------- 3) Indikator loading saat chunk Pengaturan lambat ----------
  {
    // Ruang kosong: render 300 pesan memblokir thread utama VM ini
    // cukup lama hingga spinner tidak sempat terlukis.
    await fetch(url + '/api/rooms', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: 'Ruang Kosong' }),
    });
    const ctx = await browser.newContext({ serviceWorkers: 'block' });
    const page = await ctx.newPage();
    page.setDefaultTimeout(30000);
    let toolLogBeforeClick = false;
    let clicked = false;
    page.on('request', req => {
      if (req.url().includes('/api/tool-log') && !clicked) toolLogBeforeClick = true;
    });
    // Tunda SEMUA chunk 1500 ms sejak awal: muatan awal tetap jalan
    // (lebih lambat), dan chunk Pengaturan pasti belum siap saat klik.
    await page.route('**/_app/immutable/chunks/*.js', async route => {
      await new Promise(r => setTimeout(r, 1500));
      await route.continue();
    });
    await page.goto(url);
    await page.locator('#composer').waitFor({ timeout: 30000 });
    clicked = true;
    const clickAt = Date.now();
    await page.click('#openSettings');
    const spinner = page.locator('.dialog-loading');
    await spinner.waitFor({ timeout: 5000 }).catch(() => {});
    const spinnerMs = Date.now() - clickAt;
    report.check(`spinner Pengaturan tampil saat chunk lambat (${spinnerMs} ms)`, await spinner.isVisible().catch(() => false));
    await page.locator('#settingsDialog[open]').waitFor({ timeout: 30000 });
    report.check('dialog Pengaturan akhirnya tampil', true);
    report.check('tanpa fetch /api/tool-log sebelum tombol diklik', !toolLogBeforeClick);
    await ctx.close();
  }

  // ---------- 4) Prefetch chunk saat idle (tanpa instantiate) ----------
  {
    const ctx = await browser.newContext();
    const page = await ctx.newPage();
    page.setDefaultTimeout(20000);
    const chunks = new Set();
    let toolLogFetched = false;
    page.on('request', req => {
      const u = req.url();
      if (u.includes('/_app/immutable/chunks/')) chunks.add(u.split('/').pop());
      if (u.includes('/api/tool-log')) toolLogFetched = true;
    });
    await page.goto(url);
    await page.locator('#composer').waitFor({ timeout: 20000 });
    const initialChunks = new Set(chunks);
    await page.waitForTimeout(8000); // beri waktu requestIdleCallback/setTimeout prefetch
    const prefetched = [...chunks].filter(c => !initialChunks.has(c));
    report.check(`chunk dialog di-prefetch saat idle (${prefetched.length} chunk baru)`, prefetched.length > 0);
    report.check('prefetch tidak memicu fetch /api/tool-log', !toolLogFetched);
    await ctx.close();
  }

  // ---------- 5) Indikator saat ganti ruang lambat ----------
  {
    await fetch(url + '/api/rooms', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: 'Ruang Lambat' }),
    });
    const ctx = await browser.newContext();
    const page = await ctx.newPage();
    page.setDefaultTimeout(20000);
    await page.goto(url);
    await page.locator('#composer').waitFor({ timeout: 20000 });
    await page.route('**/api/rooms/*/activate', async route => {
      await new Promise(r => setTimeout(r, 800));
      await route.continue();
    });
    await page.locator('.room-bar:not(.branch-bar) .room-chip', { hasText: 'Utama' }).click();
    const spinner = page.locator('.dialog-loading');
    await spinner.waitFor({ timeout: 3000 }).catch(() => {});
    report.check('spinner ganti ruang tampil saat aktivasi lambat', await spinner.isVisible().catch(() => false));
    await page.waitForFunction(() => document.querySelector('.room-bar:not(.branch-bar) .room-chip.active')?.textContent?.includes('Utama'), null, { timeout: 20000 });
    report.check('ruang berhasil diganti', true);
    await ctx.close();
  }
} finally {
  await browser.close();
  await gateway.stop();
  child.kill('SIGTERM'); await once(child, 'exit').catch(() => {});
  rmSync(dir, { recursive: true, force: true });
}

process.exit(report.summary() > 0 ? 1 : 0);
