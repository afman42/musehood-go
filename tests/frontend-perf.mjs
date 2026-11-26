// Cross-browser & performance frontend.
// Catatan: unduhan browser tambahan (Firefox/WebKit) diblokir kebijakan
// runtime VM ini, jadi cakupan "cross-browser" = Chromium terpasang pada
// banyak viewport, emulasi perangkat sentuh (iPhone), dan skema warna.
// Jalankan: node tests/frontend-perf.mjs
import { readdirSync, statSync } from 'node:fs';
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

  // Anggaran ukuran bundle (hasil build frontend).
  const bytes = bundleSize(join(repo, 'frontend', 'build'));
  const kb = Math.round(bytes / 1024);
  report.check(`ukuran bundle JS+CSS ${kb} KB < 350 KB`, kb < 350);

  // Timing navigasi + long tasks pada pemuatan pertama.
  const page = await browser.newPage({ viewport: { width: 1280, height: 800 } });
  await page.addInitScript(() => {
    window.__longTasks = [];
    new PerformanceObserver(list => {
      for (const entry of list.getEntries()) window.__longTasks.push(entry.duration);
    }).observe({ entryTypes: ['longtask'] });
  });
  const navStart = Date.now();
  await page.goto(app.url, { waitUntil: 'load' });
  await page.getByText('Pemilik Arman - Admin Muse - Hadir 8/8').waitFor();
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
} finally {
  await browser.close();
  await gateway.stop();
  await app.stop();
}
process.exit(report.summary() > 0 ? 1 : 0);
