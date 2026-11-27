// Visual regression frontend: screenshot skenario kunci dibandingkan
// piksel-per-piksel (pixelmatch) dengan baseline di tests/visual-baselines/.
// Perbarui baseline: node tests/frontend-visual.mjs --update
// Jalankan:          node tests/frontend-visual.mjs
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
const MAX_DIFF_RATIO = 0.001; // maks 0,1% piksel berbeda

const report = makeReporter('FRONTEND VISUAL');
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
  const context = await browser.newContext({ viewport: { width: 1280, height: 800 }, reducedMotion: 'reduce', bypassCSP: true });
  const page = await context.newPage();
  // Bekukan jam agar timestamp pesan deterministik.
  await page.clock.install({ time: new Date('2026-11-26T10:00:00+07:00') });
  await page.goto(app.url);
  await page.getByText('Pemilik Arman - Admin Muse - Hadir 8/8').waitFor();
  await page.addStyleTag({ content: '*{animation:none!important;transition:none!important}' });
  await page.waitForTimeout(300);
  await compareScreenshot(page, 'utama-kosong');

  const composer = page.getByRole('textbox', { name: 'Pesan untuk grup' });
  await composer.fill('@Plamo halo visual');
  await composer.press('Enter');
  await page.getByText('Balasan polos', { exact: true }).waitFor();
  await page.waitForTimeout(300);
  await compareScreenshot(page, 'chat-dengan-pesan');

  await page.getByRole('button', { name: 'Pengaturan grup', exact: true }).click();
  await page.locator('#settingsForm').waitFor({ state: 'visible' });
  await page.waitForTimeout(300);
  await compareScreenshot(page, 'dialog-pengaturan');
  await page.keyboard.press('Escape');

  // Mode gelap.
  await api(app.url, 'POST', '/api/settings', { settings: { darkMode: true } }, 200);
  await page.reload();
  await page.getByText('Pemilik Arman - Admin Muse - Hadir 8/8').waitFor();
  await page.addStyleTag({ content: '*{animation:none!important;transition:none!important}' });
  await page.waitForTimeout(300);
  await compareScreenshot(page, 'utama-gelap');
  await api(app.url, 'POST', '/api/settings', { settings: { darkMode: false } }, 200);
  await page.reload();
  await page.getByText('Pemilik Arman - Admin Muse - Hadir 8/8').waitFor();
  await page.addStyleTag({ content: '*{animation:none!important;transition:none!important}' });

  // --- Skenario fitur pendalaman 27 Nov 2026 ---

  // Bubble dengan kartu tool + reaksi: run_code disetujui, lalu reaksi 👍.
  await api(app.url, 'POST', '/api/settings', { seats: [{ code: 'M01', model: 'toolcode' }] }, 200);
  await composer.fill('@Plamo jalankan kode visual');
  await composer.press('Enter');
  await page.getByRole('button', { name: 'Setujui' }).waitFor({ timeout: 15000 });
  await page.getByRole('button', { name: 'Setujui' }).click();
  await page.getByText('Kode selesai diproses.', { exact: true }).waitFor({ timeout: 20000 });
  const toolMessage = page.locator('.message').filter({ hasText: 'Kode selesai diproses.' }).first();
  await toolMessage.hover();
  await toolMessage.getByRole('button', { name: 'Beri reaksi' }).click();
  await page.locator('.emoji-picker [role="menuitem"]', { hasText: '👍' }).click();
  await page.locator('.reaction-pill', { hasText: '👍 1' }).waitFor();
  await page.mouse.move(0, 0);
  await page.waitForTimeout(300);
  await compareScreenshot(page, 'bubble-tool-reaksi');

  // Mode kompak aktif pada chat yang sama.
  await api(app.url, 'POST', '/api/settings', { settings: { density: 'compact' } }, 200);
  await page.reload();
  await page.getByText('Pemilik Arman - Admin Muse - Hadir 8/8').waitFor();
  await page.addStyleTag({ content: '*{animation:none!important;transition:none!important}' });
  await page.waitForTimeout(300);
  await compareScreenshot(page, 'chat-kompak');
  await api(app.url, 'POST', '/api/settings', { settings: { density: 'normal' } }, 200);
  await page.reload();
  await page.getByText('Pemilik Arman - Admin Muse - Hadir 8/8').waitFor();
  await page.addStyleTag({ content: '*{animation:none!important;transition:none!important}' });

  // Dialog debat terbuka.
  await page.locator('#debateButton').click();
  await page.locator('#debateDialog').waitFor({ state: 'visible' });
  await page.waitForTimeout(300);
  await compareScreenshot(page, 'dialog-debat');
  await page.keyboard.press('Escape');

  // Dialog bantuan shortcut.
  await page.locator('header.group-header').click();
  await page.keyboard.press('?');
  await page.locator('#shortcutHelpDialog').waitFor({ state: 'visible' });
  await page.waitForTimeout(300);
  await compareScreenshot(page, 'dialog-shortcut');
  await page.keyboard.press('Escape');

  // Pengaturan: log audit tool terisi (run_code di atas tercatat).
  await page.getByRole('button', { name: 'Pengaturan grup', exact: true }).click();
  await page.locator('#settingsForm').waitFor({ state: 'visible' });
  const toolLogFieldset = page.locator('fieldset', { hasText: 'Log Audit Tool' });
  await toolLogFieldset.scrollIntoViewIfNeeded();
  await page.locator('table.stats-table', { hasText: 'run_code' }).first().waitFor({ timeout: 15000 });
  await page.waitForTimeout(300);
  await compareScreenshot(page, 'pengaturan-log-tool');
  await page.keyboard.press('Escape');

  // Bilah ruang: badge belum dibaca pada ruang lain + ruang terarsip tersembunyi.
  // Kembalikan model M01 ke plain agar pesan badge tidak memicu banner persetujuan.
  await api(app.url, 'POST', '/api/settings', { seats: [{ code: 'M01', model: 'plain' }] }, 200);
  await api(app.url, 'POST', '/api/rooms', { name: 'Ruang Badge', seatCodes: ['M01'] }, 201);
  await api(app.url, 'POST', '/api/rooms', { name: 'Ruang Sembunyi', seatCodes: [] }, 201);
  const roomsState = await api(app.url, 'GET', '/api/state');
  const badgeRoom = roomsState.rooms.find(r => r.name === 'Ruang Badge');
  const hiddenRoom = roomsState.rooms.find(r => r.name === 'Ruang Sembunyi');
  const utamaRoom = roomsState.rooms.find(r => r.name === 'Utama');
  await api(app.url, 'POST', `/api/rooms/${utamaRoom.id}/activate`, {}, 200);
  await api(app.url, 'POST', `/api/rooms/${hiddenRoom.id}/archive`, { archived: true }, 200);
  await api(app.url, 'POST', '/api/message', { text: '@Plamo pesan badge', roomId: badgeRoom.id }, 202);
  await page.reload();
  await page.getByText('Pemilik Arman - Admin Muse - Hadir 8/8').waitFor();
  await page.locator('.room-badge').first().waitFor({ timeout: 15000 });
  await page.addStyleTag({ content: '*{animation:none!important;transition:none!important}' });
  await page.waitForTimeout(300);
  await compareScreenshot(page, 'bilah-ruang-badge');

  // Mobile.
  await page.setViewportSize({ width: 390, height: 844 });
  await page.reload();
  await page.getByText('Pemilik Arman - Admin Muse - Hadir 8/8').waitFor();
  await page.addStyleTag({ content: '*{animation:none!important;transition:none!important}' });
  await page.waitForTimeout(300);
  await compareScreenshot(page, 'utama-mobile');
} finally {
  await browser.close();
  await gateway.stop();
  await app.stop();
}

// Layar login PIN (app terpisah dengan PIN aktif).
const pinApp = await startApp();
const pinBrowser = await launchBrowser();
try {
  await api(pinApp.url, 'POST', '/api/pin', { pin: '1234', oldPin: '' }, 200);
  const context = await pinBrowser.newContext({ viewport: { width: 1280, height: 800 }, reducedMotion: 'reduce', bypassCSP: true });
  const page = await context.newPage();
  await page.clock.install({ time: new Date('2026-11-26T10:00:00+07:00') });
  await page.goto(pinApp.url);
  await page.locator('.login-card').waitFor({ state: 'visible' });
  await page.addStyleTag({ content: '*{animation:none!important;transition:none!important}' });
  await page.waitForTimeout(300);
  await compareScreenshot(page, 'login-pin');
} finally {
  await pinBrowser.close();
  await pinApp.stop();
}
process.exit(report.summary() > 0 ? 1 : 0);
