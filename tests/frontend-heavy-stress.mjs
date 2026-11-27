// Tes stres UI berat (27 Nov 2026): 3000 pesan (render, scroll, polling),
// 50 ruang, pesan 20.000 karakter, tabel Markdown (tidak didukung parser),
// blok kode >20.000 karakter, emoji/RTL, resize viewport saat dialog
// terbuka, emulasi sentuh iPhone 13, waktu muat awal + ukuran bundle.
// Jalankan: node tests/frontend-heavy-stress.mjs
import { spawn } from 'node:child_process';
import { once } from 'node:events';
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { api, configureMockProvider, eventually, launchBrowser, makeReporter, startApp, startGateway } from './frontend-harness.mjs';

const repo = dirname(dirname(fileURLToPath(import.meta.url)));
const report = makeReporter('FRONTEND HEAVY STRESS');

async function freePortLocal() {
  const server = createServer().listen(0, '127.0.0.1');
  await once(server, 'listening');
  const port = server.address().port;
  await new Promise(resolve => server.close(resolve));
  return port;
}

async function spawnOn(dir, port) {
  const child = spawn(join(repo, 'musehood-go'), [], {
    cwd: dir,
    env: { ...process.env, DATA_DIR: join(dir, 'runtime'), PORT: String(port), HOME: join(dir, 'home') },
  });
  const url = `http://127.0.0.1:${port}`;
  const deadline = Date.now() + 15000;
  while (Date.now() < deadline) {
    if (child.exitCode !== null) throw new Error('binary berhenti prematur');
    try {
      const res = await fetch(url + '/api/state');
      if (res.ok) break;
    } catch { /* belum siap */ }
    await new Promise(r => setTimeout(r, 100));
  }
  return { child, url };
}

function seedMessages(dir, total) {
  const path = join(dir, 'runtime', 'data.json');
  const data = JSON.parse(readFileSync(path, 'utf8'));
  const ownerTpl = data.messages.find(m => m.authorType === 'owner');
  const seatTpl = data.messages.find(m => m.authorType === 'seat');
  const base = Date.parse('2026-11-26T00:00:00Z');
  const messages = [];
  for (let i = 0; i < total; i++) {
    const tpl = i % 2 === 0 ? ownerTpl : seatTpl;
    messages.push({
      ...tpl, id: crypto.randomUUID(), jobId: crypto.randomUUID(),
      text: i % 2 === 0 ? `pesan nomor ${i}` : `balasan nomor ${i}`,
      status: 'completed', createdAt: new Date(base + i * 1000).toISOString(),
    });
  }
  data.messages = messages;
  writeFileSync(path, JSON.stringify(data));
}

const gateway = await startGateway();
const browser = await launchBrowser();
const dir = mkdtempSync(join(tmpdir(), 'musehood-heavy-'));
let proc = null;

try {
  const port = await freePortLocal();
  proc = await spawnOn(dir, port);
  await configureMockProvider(proc.url, gateway.url);
  await api(proc.url, 'POST', '/api/message', { text: '@Plamo pesan benih' }, 202);
  const deadline = Date.now() + 15000;
  for (;;) {
    const state = await api(proc.url, 'GET', '/api/state');
    if (state.messages.length >= 2 && state.messages[1].status === 'completed') break;
    if (Date.now() > deadline) throw new Error('pesan benih tidak selesai');
    await new Promise(r => setTimeout(r, 150));
  }
  proc.child.kill('SIGTERM');
  await once(proc.child, 'exit').catch(() => {});
  proc = null;

  // --- 3000 pesan: render, scroll, polling ---
  seedMessages(dir, 3000);
  proc = await spawnOn(dir, port);
  const page = await browser.newPage({ viewport: { width: 1280, height: 800 } });
  const pageErrors = [];
  page.on('pageerror', e => pageErrors.push(e.message));
  const renderStart = Date.now();
  await page.goto(proc.url, { waitUntil: 'load' });
  await page.waitForFunction(() => document.querySelectorAll('.message').length >= 3000, null, { timeout: 60000 });
  const renderMs = Date.now() - renderStart;
  report.check(`render 3000 pesan ${renderMs} ms < 30000 ms`, renderMs < 30000);
  console.log(`  [stress] render3000=${renderMs}ms`);

  // Scroll ke atas lalu ke bawah; ukur long task selama polling 700ms.
  await page.evaluate(() => {
    window.__longTasks = [];
    new PerformanceObserver(list => {
      for (const e of list.getEntries()) window.__longTasks.push(e.duration);
    }).observe({ entryTypes: ['longtask'] });
  });
  const scrollStart = Date.now();
  await page.evaluate(() => {
    const el = document.querySelector('.messages, .chat-scroll, main');
    if (el) { el.scrollTop = 0; }
    window.scrollTo(0, 0);
  });
  await page.waitForTimeout(500);
  await page.evaluate(() => {
    const el = document.querySelector('.messages, .chat-scroll, main');
    if (el) { el.scrollTop = el.scrollHeight; }
    window.scrollTo(0, document.body.scrollHeight);
  });
  await page.waitForTimeout(3000); // beberapa siklus polling
  const longTasks = await page.evaluate(() => window.__longTasks || []);
  const worst = longTasks.length ? Math.max(...longTasks) : 0;
  report.check(`scroll + polling 3000 pesan: long task terburuk ${Math.round(worst)} ms < 1000 ms`, worst < 1000);
  console.log(`  [stress] scroll+poll=${Date.now() - scrollStart}ms longTasks=${longTasks.length} worst=${Math.round(worst)}ms`);

  // Ketik pada ruang 3000 pesan tetap responsif.
  const composer = page.getByRole('textbox', { name: 'Pesan untuk grup' });
  const typeStart = Date.now();
  await composer.pressSequentially('ketik uji berat', { delay: 0 });
  const typeMs = Date.now() - typeStart;
  report.check(`ketik 15 karakter pada 3000 pesan ${typeMs} ms < 5000 ms`, typeMs < 5000);
  await composer.fill('');

  // --- 50 ruang: sidebar responsif + pencarian ruang ---
  for (let i = 0; i < 49; i++) {
    await api(proc.url, 'POST', '/api/rooms', { name: `Ruang Stres ${i}` }, 201);
  }
  await page.reload();
  await page.waitForFunction(() => document.querySelectorAll('.room-chip').length >= 50, null, { timeout: 30000 });
  report.check('50 ruang tampil di sidebar', true);
  const roomSearchStart = Date.now();
  await page.locator('.room-chip', { hasText: 'Ruang Stres 48' }).first().click();
  await eventually(async () => (await page.locator('.room-chip.active').textContent())?.includes('Ruang Stres 48'), 10000);
  report.check(`pindah ke ruang ke-50 ${Date.now() - roomSearchStart} ms < 10000 ms`, Date.now() - roomSearchStart < 10000);

  // --- Konten ekstrem ---
  await page.locator('.room-chip', { hasText: 'Utama' }).first().click();
  const longText = 'kata '.repeat(3999) + 'akhir';
  await composer.fill('@Plamo ' + longText.slice(0, 19990));
  await composer.press('Enter');
  await page.waitForTimeout(1500);
  report.check('pesan ~20.000 karakter terkirim tanpa crash', pageErrors.length === 0, pageErrors.join('; '));

  await composer.fill('@Plamo tabel:\n| A | B |\n|---|---|\n| 1 | 2 |');
  await composer.press('Enter');
  await page.waitForTimeout(1500);
  report.check('tabel Markdown (tidak didukung parser) tidak crash', pageErrors.length === 0, pageErrors.join('; '));

  const bigCode = '```js\n' + 'console.log("baris");\n'.repeat(1200) + '```';
  await page.evaluate((text) => {
    const el = document.querySelector('#messageInput');
    el.focus();
    document.execCommand('insertText', false, text);
  }, '@Plamo kode besar:\n' + bigCode.slice(0, 19900));
  await composer.press('Enter');
  await page.waitForTimeout(2000);
  const codeBlocks = await page.locator('.message pre, .message code').count();
  report.check('blok kode >20.000 karakter tampil (tanpa highlight)', codeBlocks > 0 && pageErrors.length === 0);

  await composer.fill('@Plamo emoji 🫠🔥 RTL مرحبا بالعالم campuran');
  await composer.press('Enter');
  await page.getByText(/مرحبا بالعالم/).first().waitFor({ timeout: 10000 });
  report.check('emoji + RTL tampil', true);

  // --- Resize viewport saat dialog terbuka ---
  await page.locator('#abTestButton').click();
  await page.locator('#abTestDialog').waitFor({ state: 'visible' });
  for (const vp of [{ width: 360, height: 640 }, { width: 768, height: 1024 }, { width: 1440, height: 900 }]) {
    await page.setViewportSize(vp);
    await page.waitForTimeout(300);
    const visible = await page.locator('#abTestDialog').isVisible();
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth + 1);
    report.check(`dialog A/B tetap tampil tanpa overflow di ${vp.width}px`, visible && !overflow);
  }
  await page.keyboard.press('Escape');

  // --- Waktu muat awal + ukuran bundle ---
  const nav = await page.evaluate(() => {
    const e = performance.getEntriesByType('navigation')[0];
    return e ? Math.round(e.domInteractive) : -1;
  });
  report.check(`DOM interaktif ${nav} ms < 10000 ms`, nav > 0 && nav < 10000);
  const bundleBytes = await page.evaluate(async () => {
    const entries = performance.getEntriesByType('resource').filter(r => r.name.endsWith('.js') || r.name.endsWith('.css'));
    return entries.reduce((s, r) => s + (r.transferSize || r.encodedBodySize || 0), 0);
  });
  report.check(`bundle JS+CSS tertransfer ${Math.round(bundleBytes / 1024)} KB < 600 KB`, bundleBytes < 600 * 1024);
  console.log(`  [stress] domInteractive=${nav}ms bundle=${Math.round(bundleBytes / 1024)}KB`);

  await page.close();
  proc.child.kill('SIGTERM');
  await once(proc.child, 'exit').catch(() => {});
  proc = null;

  // --- Emulasi sentuh iPhone 13: alur kirim pesan + dialog ---
  const app2 = await startApp();
  try {
    await configureMockProvider(app2.url, gateway.url);
    const iphone = await browser.newContext({
      viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true,
      userAgent: 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1',
    });
    const touchPage = await iphone.newPage();
    await touchPage.goto(app2.url);
    await touchPage.getByText('Pemilik Arman - Admin Muse - Hadir 8/8').waitFor();
    const touchComposer = touchPage.getByRole('textbox', { name: 'Pesan untuk grup' });
    await touchComposer.tap();
    await touchComposer.fill('@Plamo halo sentuh');
    await touchPage.locator('#sendButton').tap();
    await touchPage.getByText('Balasan polos', { exact: true }).waitFor({ timeout: 15000 });
    report.check('iPhone 13 (sentuh): kirim pesan + balasan tampil', true);
    await touchPage.locator('#abTestButton').tap();
    await touchPage.locator('#abTestDialog').waitFor({ state: 'visible' });
    const overflow = await touchPage.evaluate(() => document.documentElement.scrollWidth > window.innerWidth + 1);
    report.check('iPhone 13: dialog A/B tanpa overflow horizontal', !overflow);
    await iphone.close();
  } finally {
    await app2.stop();
  }
} finally {
  if (proc) { proc.child.kill('SIGTERM'); await once(proc.child, 'exit').catch(() => {}); }
  rmSync(dir, { recursive: true, force: true });
  await browser.close();
  await gateway.stop();
}
process.exit(report.summary() ? 1 : 0);
