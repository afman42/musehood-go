// Cross-browser & performance frontend.
// Catatan: unduhan browser tambahan (Firefox/WebKit) diblokir kebijakan
// runtime VM ini, jadi cakupan "cross-browser" = Chromium terpasang pada
// banyak viewport, emulasi perangkat sentuh (iPhone), dan skema warna.
// Jalankan: node tests/frontend-perf.mjs
import { spawn } from 'node:child_process';
import { once } from 'node:events';
import { mkdtempSync, readFileSync, rmSync, statSync, writeFileSync, readdirSync } from 'node:fs';
import { createServer } from 'node:net';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { devices } from '../frontend/node_modules/playwright/index.mjs';
import { api, configureMockProvider, launchBrowser, makeReporter, startApp, startGateway } from './frontend-harness.mjs';

const repo = dirname(dirname(fileURLToPath(import.meta.url)));
const report = makeReporter('FRONTEND PERF & CROSS-BROWSER');
const app = await startApp();
const gateway = await startGateway();
const browser = await launchBrowser();

function bundleSize(dir) {
  let total = 0;
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) total += bundleSize(path);
    else if (/\.(js|css)$/.test(entry.name)) total += statSync(path).size;
  }
  return total;
}

try {
  await configureMockProvider(app.url, gateway.url);

  // Timing navigasi + long tasks pada pemuatan pertama.
  // Sekalian kumpulkan aset JS+CSS yang benar-benar diunduh pada
  // pemuatan pertama untuk anggaran muatan AWAL: chunk malas
  // (Pengaturan/dialog) tidak dihitung karena baru dimuat saat dibuka.
  const initialAssets = new Set();
  const page = await browser.newPage({ viewport: { width: 1280, height: 800 } });
  page.on('response', res => {
    const url = res.url();
    if (/\.(js|css)(\?|$)/.test(url)) initialAssets.add(new URL(url).pathname);
  });
  await page.addInitScript(() => {
    window.__longTasks = [];
    new PerformanceObserver(list => {
      for (const entry of list.getEntries()) window.__longTasks.push(entry.duration);
    }).observe({ entryTypes: ['longtask'] });
  });
  const navStart = Date.now();
  await page.goto(app.url, { waitUntil: 'load' });
  await page.getByText('Pemilik Arman - Admin Muse - Hadir 8/8').waitFor();

  // Anggaran ukuran muatan awal (bukan total semua chunk): jumlah byte
  // file JS+CSS di frontend/build yang diunduh browser di atas.
  let initialBytes = 0;
  for (const pathname of initialAssets) {
    try { initialBytes += statSync(join(repo, 'frontend', 'build', pathname)).size; } catch { /* aset non-file */ }
  }
  const initialKb = Math.round(initialBytes / 1024);
  report.check(`muatan awal JS+CSS ${initialKb} KB < 350 KB`, initialKb < 350);
  const totalKb = Math.round(bundleSize(join(repo, 'frontend', 'build')) / 1024);
  console.log(`  [perf] muatan awal ${initialKb} KB dari ${initialAssets.size} aset; total semua chunk JS+CSS ${totalKb} KB (termasuk chunk malas)`);
  const interactiveMs = Date.now() - navStart;
  const timing = await page.evaluate(() => {
    const nav = performance.getEntriesByType('navigation')[0];
    return {
      domContentLoaded: Math.round(nav.domContentLoadedEventEnd),
      load: Math.round(nav.loadEventEnd),
      longTasks: window.__longTasks || [],
    };
  });
  report.check(`DOMContentLoaded ${timing.domContentLoaded} ms < 2000 ms`, timing.domContentLoaded < 2000);
  report.check(`load ${timing.load} ms < 4000 ms`, timing.load < 4000);
  report.check(`konten interaktif ${interactiveMs} ms < 2500 ms`, interactiveMs < 2500);
  const longTotal = Math.round(timing.longTasks.reduce((a, b) => a + b, 0));
  report.check(`total long task ${longTotal} ms < 500 ms`, longTotal < 500);

  // Kadens polling /api/state (~700 ms saat terlihat).
  let stateRequests = 0;
  page.on('request', req => { if (req.url().includes('/api/state')) stateRequests++; });
  await page.waitForTimeout(3500);
  report.check(`polling /api/state ${stateRequests}x dalam 3,5 dtk (3-7)`, stateRequests >= 3 && stateRequests <= 7);

  // Render banyak pesan setelah reload.
  for (let i = 0; i < 10; i++) {
    await api(app.url, 'POST', '/api/message', { text: `@Plamo pesan perf ${i}` }, 202);
  }
  await page.waitForTimeout(1500);
  const renderStart = Date.now();
  await page.reload({ waitUntil: 'load' });
  await page.locator('.message').nth(19).waitFor();
  const renderMs = Date.now() - renderStart;
  report.check(`render 20 pesan ${renderMs} ms < 2000 ms`, renderMs < 2000);

  // Cross-viewport: tanpa overflow horizontal, composer terlihat.
  for (const width of [320, 390, 768, 1280, 1920]) {
    await page.setViewportSize({ width, height: 800 });
    await page.waitForTimeout(150);
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    const composerVisible = await page.getByRole('textbox', { name: 'Pesan untuk grup' }).isVisible();
    report.check(`viewport ${width}px tanpa overflow (lebih ${overflow}px)`, overflow <= 0 && composerVisible);
  }

  // Emulasi perangkat sentuh (iPhone 13): ketuk untuk mengirim pesan.
  const iphone = devices['iPhone 13'];
  const touchContext = await browser.newContext({ ...iphone });
  const touchPage = await touchContext.newPage();
  await touchPage.goto(app.url);
  await touchPage.getByText('Pemilik Arman - Admin Muse - Hadir 8/8').waitFor();
  const touchComposer = touchPage.getByRole('textbox', { name: 'Pesan untuk grup' });
  await touchComposer.tap();
  await touchComposer.fill('@Plamo dari sentuh');
  await touchPage.getByRole('button', { name: /kirim/i }).tap().catch(async () => {
    await touchComposer.press('Enter');
  });
  await touchPage.getByText('Balasan polos', { exact: true }).first().waitFor();
  report.check('perangkat sentuh (iPhone 13) bisa mengirim pesan', true);
  await touchContext.close();

  // Skema warna gelap lewat emulasi media tidak merusak layout.
  const darkContext = await browser.newContext({ colorScheme: 'dark', viewport: { width: 390, height: 844 } });
  const darkPage = await darkContext.newPage();
  await darkPage.goto(app.url);
  await darkPage.getByText('Pemilik Arman - Admin Muse - Hadir 8/8').waitFor();
  const darkOverflow = await darkPage.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  report.check('skema gelap + mobile tanpa overflow', darkOverflow <= 0);
  await darkContext.close();

  // Permukaan baru (dialog debat, bantuan shortcut, pengaturan) pada
  // matriks viewport: dialog tidak melebihi lebar layar, tanpa overflow.
  const fitsViewport = async (locator, width) => {
    const box = await locator.boundingBox();
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    return box !== null && box.width <= width + 1 && box.x >= -1 && overflow <= 0;
  };
  for (const width of [1280, 768, 390]) {
    await page.setViewportSize({ width, height: 844 });
    await page.locator('#debateButton').click();
    const debateDialog = page.getByRole('dialog', { name: 'Mode Debat' });
    await debateDialog.waitFor();
    report.check(`dialog debat muat di viewport ${width}px`, await fitsViewport(debateDialog, width));
    await page.keyboard.press('Escape');
    await debateDialog.waitFor({ state: 'hidden' });
    // Pastikan fokus bukan di input/textarea agar '?' membuka bantuan.
    await page.evaluate(() => { document.activeElement?.blur(); document.body.focus(); });
    await page.keyboard.press('?');
    const helpDialog = page.getByRole('dialog', { name: 'Shortcut Keyboard' });
    await helpDialog.waitFor();
    report.check(`dialog bantuan shortcut muat di viewport ${width}px`, await fitsViewport(helpDialog, width));
    await page.keyboard.press('Escape');
  }
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole('button', { name: 'Pengaturan grup' }).click();
  const settingsDialog = page.getByRole('dialog', { name: 'Pengaturan grup' });
  await settingsDialog.waitFor();
  report.check('dialog pengaturan muat di viewport 390px', await fitsViewport(settingsDialog, 390));
  await page.keyboard.press('Escape');

  // Perangkat sentuh: dialog debat bisa dibuka dan muat di layar iPhone.
  const touchDebateContext = await browser.newContext({ ...devices['iPhone 13'] });
  const touchDebatePage = await touchDebateContext.newPage();
  await touchDebatePage.goto(app.url);
  await touchDebatePage.getByText('Pemilik Arman - Admin Muse - Hadir 8/8').waitFor();
  await touchDebatePage.locator('#debateButton').tap();
  const touchDebate = touchDebatePage.getByRole('dialog', { name: 'Mode Debat' });
  await touchDebate.waitFor();
  const touchBox = await touchDebate.boundingBox();
  const touchOverflow = await touchDebatePage.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  report.check('dialog debat muat di iPhone 13 (sentuh)', touchBox !== null && touchBox.width <= 391 && touchOverflow <= 0);
  await touchDebateContext.close();
} finally {
  await browser.close();
  await gateway.stop();
  await app.stop();
}

// --- Layar login PIN pada matriks viewport (app terpisah dengan PIN) ---
{
  const pinApp = await startApp();
  const pinBrowser = await launchBrowser();
  try {
    await api(pinApp.url, 'POST', '/api/pin', { pin: '1234', oldPin: '' }, 200);
    for (const width of [1280, 768, 390]) {
      const ctx = await pinBrowser.newContext({ viewport: { width, height: 844 } });
      const loginPage = await ctx.newPage();
      await loginPage.goto(pinApp.url);
      const card = loginPage.locator('.login-card');
      await card.waitFor();
      const box = await card.boundingBox();
      const overflow = await loginPage.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
      report.check(`layar login PIN muat di viewport ${width}px`, box !== null && box.width <= width + 1 && overflow <= 0);
      await ctx.close();
    }
    const touchCtx = await pinBrowser.newContext({ ...devices['iPhone 13'] });
    const touchLogin = await touchCtx.newPage();
    await touchLogin.goto(pinApp.url);
    await touchLogin.locator('.login-card').waitFor();
    await touchLogin.locator('.login-card').getByLabel('PIN').tap();
    await touchLogin.locator('.login-card').getByLabel('PIN').fill('1234');
    await touchLogin.getByRole('button', { name: 'Masuk' }).tap();
    await touchLogin.getByText('Pemilik Arman - Admin Muse - Hadir 8/8').waitFor();
    report.check('login PIN lewat sentuh iPhone 13 berhasil', true);
    await touchCtx.close();
  } finally {
    await pinBrowser.close();
    await pinApp.stop();
  }
}

// --- Ruang besar: render 300/3000 pesan, latensi ketik, pencarian, log tool ---
// Data di-seed langsung ke data.json (app dimatikan dulu) agar cepat;
// pesan hasil kloning dari pesan asli yang dibuat lewat API.
async function spawnOn(dir, port) {
  const child = spawn(join(repo, 'musehood-go'), [], {
    cwd: dir,
    env: { ...process.env, DATA_DIR: join(dir, 'runtime'), PORT: String(port), HOME: join(dir, 'home') },
  });
  const url = `http://127.0.0.1:${port}`;
  const deadline = Date.now() + 15000;
  while (Date.now() < deadline) {
    if (child.exitCode !== null) throw new Error('binary ruang besar berhenti prematur');
    try {
      const res = await fetch(url + '/api/state');
      if (res.ok) break;
    } catch { /* belum siap */ }
    await new Promise(r => setTimeout(r, 100));
  }
  return { child, url };
}

async function freePortLocal() {
  const server = createServer().listen(0, '127.0.0.1');
  await once(server, 'listening');
  const port = server.address().port;
  await new Promise(resolve => server.close(resolve));
  return port;
}

function seedMessages(dir, total, toolEntries) {
  const path = join(dir, 'runtime', 'data.json');
  const data = JSON.parse(readFileSync(path, 'utf8'));
  const ownerTpl = data.messages.find(m => m.authorType === 'owner');
  const seatTpl = data.messages.find(m => m.authorType === 'seat');
  const base = Date.parse('2026-11-26T00:00:00Z');
  const messages = [];
  for (let i = 0; i < total; i++) {
    const tpl = i % 2 === 0 ? ownerTpl : seatTpl;
    messages.push({
      ...tpl,
      id: crypto.randomUUID(),
      jobId: crypto.randomUUID(),
      text: i % 2 === 0 ? `pesan nomor ${i}` : `balasan nomor ${i}`,
      status: 'completed',
      createdAt: new Date(base + i * 1000).toISOString(),
    });
  }
  data.messages = messages;
  data.toolLog = Array.from({ length: toolEntries }, (_, i) => ({
    time: new Date(base + i * 1000).toISOString(),
    seatCode: 'M01', seatName: 'Plamo', tool: 'run_code',
    arg: `python langkah ${i}`, ok: true, durationMs: 42,
  }));
  writeFileSync(path, JSON.stringify(data));
}

{
  const bigGateway = await startGateway();
  const bigBrowser = await launchBrowser();
  const dir = mkdtempSync(join(tmpdir(), 'musehood-big-'));
  let proc = null;
  try {
    const port = await freePortLocal();
    proc = await spawnOn(dir, port);
    await configureMockProvider(proc.url, bigGateway.url);
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

    const measureRender = async (density) => {
      await api(proc.url, 'POST', '/api/settings', { settings: { density } }, 200);
      const page = await bigBrowser.newPage({ viewport: { width: 1280, height: 800 } });
      const start = Date.now();
      await page.goto(proc.url, { waitUntil: 'load' });
      await page.waitForFunction(() => document.querySelectorAll('.message').length >= 300, null, { timeout: 30000 });
      const ms = Date.now() - start;
      await page.close();
      return ms;
    };

    // 300 pesan: mode normal vs kompak.
    seedMessages(dir, 300, 0);
    proc = await spawnOn(dir, port);
    const normal300 = await measureRender('normal');
    report.check(`render 300 pesan (normal) ${normal300} ms < 8000 ms`, normal300 < 8000);
    const compact300 = await measureRender('compact');
    report.check(`render 300 pesan (kompak) ${compact300} ms < 8000 ms`, compact300 < 8000);
    console.log(`  [perf] render300 normal=${normal300}ms kompak=${compact300}ms`);
    proc.child.kill('SIGTERM');
    await once(proc.child, 'exit').catch(() => {});
    proc = null;

    // 3000 pesan + 500 entri log tool.
    seedMessages(dir, 3000, 500);
    proc = await spawnOn(dir, port);
    await api(proc.url, 'POST', '/api/settings', { settings: { density: 'normal' } }, 200);
    const page = await bigBrowser.newPage({ viewport: { width: 1280, height: 800 } });
    await page.addInitScript(() => {
      window.__longTasks = [];
      new PerformanceObserver(list => {
        for (const entry of list.getEntries()) window.__longTasks.push(Math.round(entry.duration));
      }).observe({ entryTypes: ['longtask'] });
    });
    const renderStart = Date.now();
    await page.goto(proc.url, { waitUntil: 'load' });
    await page.waitForFunction(() => document.querySelectorAll('.message').length >= 3000, null, { timeout: 60000 });
    const render3000 = Date.now() - renderStart;
    report.check(`render 3000 pesan ${render3000} ms < 30000 ms`, render3000 < 30000);
    console.log(`  [perf] render3000=${render3000}ms`);

    // Polling idle tidak boleh memblokir thread utama: state yang tidak
    // berubah tidak me-render ulang daftar pesan (penjaga regresi untuk
    // signature state di chat.svelte.ts).
    await page.evaluate(() => { window.__longTasks = []; });
    await page.waitForTimeout(3500);
    const idleLong = await page.evaluate(() => window.__longTasks);
    const idleTotal = idleLong.reduce((a, b) => a + b, 0);
    report.check(`idle 3,5 dtk dengan 3000 pesan: ${idleLong.length} long task (total ${idleTotal} ms) < 400 ms`, idleTotal < 400);
    console.log(`  [perf] idleLong3000=${JSON.stringify(idleLong)}`);

    // Latensi ketik di composer dengan 3000 pesan ter-render. Angka
    // absolut didominasi biaya produksi frame browser untuk DOM besar
    // di VM ini; ambang longgar sebagai penjaga regresi kasar.
    const composer = page.getByRole('textbox', { name: 'Pesan untuk grup' });
    await composer.click();
    const typed = 'x'.repeat(60);
    const typeStart = Date.now();
    await page.keyboard.type(typed);
    const typeMs = Date.now() - typeStart;
    const value = await composer.inputValue();
    report.check(`ketik 60 karakter dengan 3000 pesan ${typeMs} ms (${(typeMs / 60).toFixed(1)} ms/karakter) < 15000 ms`, value === typed && typeMs < 15000);
    console.log(`  [perf] ketik60=${typeMs}ms (${(typeMs / 60).toFixed(1)}ms/karakter)`);

    // Pencarian Ctrl+K di ruang besar.
    const searchStart = Date.now();
    await page.keyboard.press('Control+K');
    await page.getByRole('searchbox', { name: 'Cari pesan' }).fill('nomor 2999');
    await page.waitForFunction(() => document.querySelectorAll('.message mark').length > 0, null, { timeout: 10000 });
    const searchMs = Date.now() - searchStart;
    report.check(`pencarian Ctrl+K di ruang 3000 pesan ${searchMs} ms < 5000 ms`, searchMs < 5000);
    console.log(`  [perf] ctrlK=${searchMs}ms`);
    await page.keyboard.press('Escape');

    // Buka pengaturan: log tool 500 entri (UI memuat 100 terakhir).
    const logStart = Date.now();
    await page.getByRole('button', { name: 'Pengaturan grup' }).click();
    const toolFieldset = page.locator('fieldset', { hasText: 'Log Audit Tool' });
    await toolFieldset.scrollIntoViewIfNeeded();
    await page.waitForFunction(
      () => document.querySelectorAll('fieldset table.stats-table tbody tr').length >= 100,
      null, { timeout: 15000 },
    );
    const logMs = Date.now() - logStart;
    report.check(`buka log tool (500 entri tersimpan) ${logMs} ms < 5000 ms`, logMs < 5000);
    console.log(`  [perf] logTool500=${logMs}ms`);
    await page.close();
  } finally {
    if (proc) { proc.child.kill('SIGTERM'); await once(proc.child, 'exit').catch(() => {}); }
    await bigBrowser.close();
    await bigGateway.stop();
    rmSync(dir, { recursive: true, force: true });
  }
}
process.exit(report.summary() > 0 ? 1 : 0);
