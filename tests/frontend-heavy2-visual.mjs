// Visual regression berat ronde 2 (28 Nov 2026): baseline BARU untuk
// panel SettingsOmp, RoomWorkPanel, QuestionBanner, SettingsMCP —
// terang + gelap. Baseline lama tidak diubah.
// Buat/perbarui baseline: node tests/frontend-heavy2-visual.mjs --update
// Jalankan: node tests/frontend-heavy2-visual.mjs
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import pixelmatch from '../frontend/node_modules/pixelmatch/index.js';
import { PNG } from '../frontend/node_modules/pngjs/lib/png.js';
import { api, configureMockProvider, launchBrowser, makeReporter, startApp, startGateway } from './frontend-harness.mjs';

const repo = dirname(dirname(fileURLToPath(import.meta.url)));
const baselineDir = join(repo, 'tests', 'visual-baselines');
const diffDir = join(repo, 'tests', 'visual-diffs');
mkdirSync(baselineDir, { recursive: true });
mkdirSync(diffDir, { recursive: true });
const update = process.argv.includes('--update');
const MAX_DIFF_RATIO = 0.001;

const report = makeReporter('FRONTEND HEAVY2 VISUAL');
const app = await startApp();
const gateway = await startGateway();
const browser = await launchBrowser();

async function compareScreenshot(page, name, locator) {
  const shot = locator
    ? await locator.screenshot({ animations: 'disabled' })
    : await page.screenshot({ animations: 'disabled' });
  const baselinePath = join(baselineDir, `${name}.png`);
  if (update || !existsSync(baselinePath)) {
    writeFileSync(baselinePath, shot);
    report.check(`baseline ${name} ${update ? 'diperbarui' : 'dibuat'}`, true);
    return;
  }
  const expected = PNG.sync.read(readFileSync(baselinePath));
  const actual = PNG.sync.read(shot);
  if (expected.width !== actual.width || expected.height !== actual.height) {
    report.check(`visual ${name}`, false, `ukuran berubah ${expected.width}x${expected.height} -> ${actual.width}x${actual.height}`);
    writeFileSync(join(diffDir, `${name}-actual.png`), shot);
    return;
  }
  const diff = new PNG({ width: expected.width, height: expected.height });
  const diffPixels = pixelmatch(expected.data, actual.data, diff.data, expected.width, expected.height, { threshold: 0.1 });
  const ratio = diffPixels / (expected.width * expected.height);
  if (ratio > MAX_DIFF_RATIO) {
    writeFileSync(join(diffDir, `${name}-diff.png`), PNG.sync.write(diff));
    writeFileSync(join(diffDir, `${name}-actual.png`), shot);
  }
  report.check(`visual ${name} (beda ${(ratio * 100).toFixed(3)}%)`, ratio <= MAX_DIFF_RATIO,
    `diff: tests/visual-diffs/${name}-diff.png`);
}

try {
  await configureMockProvider(app.url, gateway.url, { M01: 'plain', M02: 'sse', M10: 'array', M04: 'toolquestion' });
  await api(app.url, 'POST', '/api/settings', { settings: { integratorSeat: 'M10' } }, 200);
  const state0 = await api(app.url, 'GET', '/api/state', undefined, 200);
  const roomID = (state0.rooms ?? []).find(r => r.active)?.id ?? 'room-utama';
  await api(app.url, 'POST', `/api/rooms/${roomID}/todos`, { text: 'todo visual satu' }, 201);
  await api(app.url, 'POST', `/api/rooms/${roomID}/todos`, { text: 'todo visual dua' }, 201);

  for (const theme of ['light', 'dark']) {
    await api(app.url, 'POST', '/api/settings', { settings: { darkMode: theme === 'dark' } }, 200);
    const context = await browser.newContext({ viewport: { width: 1280, height: 800 }, reducedMotion: 'reduce', bypassCSP: true });
    const page = await context.newPage();
    page.on('dialog', d => d.accept());
    await page.clock.install({ time: new Date('2026-11-28T10:00:00+07:00') });
    await page.goto(app.url);
    await page.getByText('Pemilik Arman - Admin Muse - Hadir 8/8').waitFor();
    await page.addStyleTag({ content: '*{animation:none!important;transition:none!important}' });
    const composer = page.getByRole('textbox', { name: 'Pesan untuk grup' });

    // RoomWorkPanel terbuka dengan todo.
    await page.locator('.work-toggle').click();
    await page.locator('.work-body').waitFor({ state: 'visible' });
    await page.waitForTimeout(300);
    await compareScreenshot(page, `heavy2-roomwork-${theme}`);

    // QuestionBanner.
    await composer.fill('@Deepseek tanya warna');
    await composer.press('Enter');
    await page.locator('.question-banner').waitFor({ state: 'visible', timeout: 20000 });
    await page.waitForTimeout(300);
    await compareScreenshot(page, `heavy2-question-${theme}`);
    await page.locator('.question-options button').first().click();
    await page.locator('.question-banner').waitFor({ state: 'hidden', timeout: 10000 });

    // Bagian Tool agen (omp) + Server MCP: screenshot elemen section agar
    // deterministik (posisi scroll dialog berubah antar-run).
    await page.getByRole('button', { name: 'Pengaturan grup', exact: true }).click();
    await page.locator('#settingsForm').waitFor({ state: 'visible' });
    await page.locator('select[aria-label="Kebijakan tool write_file"]').waitFor({ state: 'attached', timeout: 15000 });
    await page.waitForTimeout(500);
    const ompSection = page.locator('section.settings-section').filter({ has: page.getByRole('heading', { name: 'Tool agen (omp)' }) });
    await compareScreenshot(page, `heavy2-settings-omp-${theme}`, ompSection);
    const mcpSection = page.locator('section.settings-section').filter({ has: page.getByRole('heading', { name: 'Server MCP' }) });
    await compareScreenshot(page, `heavy2-settings-mcp-${theme}`, mcpSection);
    await page.keyboard.press('Escape');

    await context.close();
  }
} catch (error) {
  report.check('visual selesai tanpa error fatal', false, String(error).slice(0, 300));
} finally {
  await browser.close().catch(() => {});
  await gateway.stop().catch(() => {});
  await app.stop().catch(() => {});
}

process.exit(report.summary() > 0 ? 1 : 0);
