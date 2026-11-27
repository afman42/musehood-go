// Visual regression berat (27 Nov 2026): baseline baru untuk dialog Uji
// A/B, panel Rantai Prompt, banner offline, dan kartu rating — terang +
// gelap. Buat/perbarui baseline: node tests/frontend-visual-heavy.mjs --update
// Jalankan: node tests/frontend-visual-heavy.mjs
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

const report = makeReporter('FRONTEND VISUAL HEAVY');
const app = await startApp();
const gateway = await startGateway();
const browser = await launchBrowser();

async function compareScreenshot(page, name) {
  const shot = await page.screenshot({ animations: 'disabled' });
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
  await configureMockProvider(app.url, gateway.url);
  await api(app.url, 'POST', '/api/chains', { name: 'Rantai Visual', steps: [{ seatCode: 'M01', instruction: 'uji visual' }] }, 201);

  for (const theme of ['light', 'dark']) {
    await api(app.url, 'POST', '/api/settings', { settings: { darkMode: theme === 'dark' } }, 200);
    const context = await browser.newContext({ viewport: { width: 1280, height: 800 }, reducedMotion: 'reduce', bypassCSP: true });
    const page = await context.newPage();
    page.on('dialog', d => d.accept());
    await page.clock.install({ time: new Date('2026-11-27T10:00:00+07:00') });
    await page.goto(app.url);
    await page.getByText('Pemilik Arman - Admin Muse - Hadir 8/8').waitFor();
    await page.addStyleTag({ content: '*{animation:none!important;transition:none!important}' });
    const composer = page.getByRole('textbox', { name: 'Pesan untuk grup' });

    // Kartu rating: balasan kursi dengan tombol rating.
    await composer.fill('@Plamo pesan visual');
    await composer.press('Enter');
    await page.getByText('Balasan polos', { exact: true }).waitFor({ timeout: 15000 });
    await page.locator('.message:not(.message-owner)').first().hover();
    await page.waitForTimeout(300);
    await compareScreenshot(page, `heavy-rating-${theme}`);

    // Dialog Uji A/B.
    await page.locator('#abTestButton').click();
    await page.locator('#abTestDialog').waitFor({ state: 'visible' });
    await page.waitForTimeout(300);
    await compareScreenshot(page, `heavy-ab-${theme}`);
    await page.keyboard.press('Escape');

    // Panel Rantai Prompt di Pengaturan.
    await page.getByRole('button', { name: 'Pengaturan grup', exact: true }).click();
    await page.locator('#settingsForm').waitFor({ state: 'visible' });
    await page.getByText('Rantai Visual').first().scrollIntoViewIfNeeded().catch(() => {});
    await page.waitForTimeout(400);
    await compareScreenshot(page, `heavy-rantai-${theme}`);
    await page.keyboard.press('Escape');

    // Banner offline.
    await context.setOffline(true);
    await page.locator('.offline-banner').waitFor({ state: 'visible', timeout: 10000 });
    await page.waitForTimeout(300);
    await compareScreenshot(page, `heavy-offline-${theme}`);
    await context.setOffline(false);

    await context.close();
  }
} finally {
  await browser.close();
  await gateway.stop();
  await app.stop();
}
process.exit(report.summary() ? 1 : 0);
