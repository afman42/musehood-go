// E2E integrasi pengaturan untuk fitur pendalaman 27 Nov 2026:
// persona preset per kursi, pasang skill dari URL (jalur penolakan
// SSRF/HTTPS — jalur sukses butuh URL publik, diuji backend),
// kepadatan kompak, toggle cache respons, badge health provider,
// tabel uji latency, dan log audit tool terisi dari alur chat nyata.
// Jalankan: node tests/frontend-e2e-depth-settings.mjs
import { api, configureMockProvider, eventually, launchBrowser, makeReporter, startApp, startGateway } from './frontend-harness.mjs';

const report = makeReporter('FRONTEND E2E DEPTH SETTINGS');
const app = await startApp();
const gateway = await startGateway();
const browser = await launchBrowser();

try {
  const providerId = await configureMockProvider(app.url, gateway.url, { M01: 'plain', M02: 'sse' });
  const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
  const pageErrors = [];
  page.on('pageerror', e => pageErrors.push(e.message));
  await page.goto(app.url);
  await page.getByText('Pemilik Arman - Admin Muse - Hadir 8/8').waitFor();
  const composer = page.getByRole('textbox', { name: 'Pesan untuk grup' });

  async function openSettings() {
    await page.getByRole('button', { name: 'Pengaturan grup', exact: true }).click();
    await page.locator('#settingsForm').waitFor({ state: 'visible' });
  }
  async function closeSettings() {
    await page.getByRole('button', { name: 'Tutup pengaturan' }).click();
    await eventually(async () => (await page.locator('#settingsForm').isVisible().catch(() => false)) === false);
  }

  // 1. Persona preset: terapkan ke M01 -> peran berubah di server + kartu kursi.
  await openSettings();
  const personaSelect = page.getByLabel('Persona M01');
  await personaSelect.waitFor();
  await personaSelect.selectOption('reviewer-ketat');
  await eventually(async () => {
    const state = await api(app.url, 'GET', '/api/state');
    return state.seats.find(s => s.code === 'M01')?.role === 'Peninjau Kritis';
  });
  report.check('persona reviewer-ketat diterapkan (peran Peninjau Kritis)', true);
  await closeSettings();
  await page.locator('.seat-card', { hasText: 'Plamo' }).getByText('Peninjau Kritis').waitFor();
  report.check('peran persona tampil di kartu kursi', true);

  // 2. Pasang skill: URL non-HTTPS dan URL privat ditolak dengan pesan error.
  await openSettings();
  const skillUrl = page.getByLabel('URL skill');
  await skillUrl.fill('http://example.com/SKILL.md');
  await page.getByRole('button', { name: 'Pasang Skill', exact: true }).click();
  await page.getByText('URL skill harus berupa URL HTTPS yang valid.').waitFor();
  report.check('pasang skill URL non-HTTPS ditolak dengan pesan', true);
  await skillUrl.fill('https://127.0.0.1/SKILL.md');
  await page.getByRole('button', { name: 'Pasang Skill', exact: true }).click();
  await page.getByText('URL skill ditolak penjaga SSRF.').waitFor();
  report.check('pasang skill URL privat ditolak penjaga SSRF', true);

  // 3. Kepadatan kompak + cache respons: simpan -> kelas html + state server.
  await page.locator('#densitySelect').selectOption('compact');
  await page.locator('#responseCacheEnabled').check();
  await page.locator('#chainReplies').check();
  await page.locator('#saveSettings').click();
  await page.locator('#saveStatus').getByText('Tersimpan').waitFor();
  await eventually(async () => {
    const state = await api(app.url, 'GET', '/api/state');
    return state.settings.density === 'compact'
      && state.settings.responseCacheEnabled === true
      && state.settings.chainReplies === true;
  });
  report.check('kepadatan kompak + cache + rantai tersimpan ke server', true);
  await closeSettings();
  await eventually(async () => page.evaluate(() => document.documentElement.classList.contains('density-compact')));
  report.check('kelas density-compact aktif di html', true);

  // 4. Health provider: tombol Cek Kesehatan -> badge sehat.
  await openSettings();
  await page.getByRole('button', { name: 'Cek Kesehatan', exact: true }).first().click();
  await page.locator('.health-badge.ok').first().waitFor({ timeout: 15000 });
  const badgeText = await page.locator('.health-badge.ok').first().textContent();
  report.check('badge health provider tampil sehat', /Sehat \d+ mdtk/.test(badgeText), badgeText);

  // 5. Uji latency: tabel hasil terurut tampil.
  await page.getByRole('button', { name: 'Uji Latency', exact: true }).first().click();
  await page.locator('.latency-table, table').filter({ hasText: 'plain' }).first().waitFor({ timeout: 30000 });
  const latencyText = await page.locator('#settingsForm').textContent();
  report.check('tabel uji latency tampil dengan model mock', latencyText.includes('plain') && /mdtk/.test(latencyText));
  await closeSettings();

  // 6. Log audit tool terisi dari alur chat nyata (run_code disetujui).
  await api(app.url, 'POST', '/api/settings', { seats: [{ code: 'M01', model: 'toolcode' }] }, 200);
  await composer.fill('@Plamo jalankan kode untuk log');
  await composer.press('Enter');
  await page.getByRole('button', { name: 'Setujui' }).waitFor({ timeout: 15000 });
  await page.getByRole('button', { name: 'Setujui' }).click();
  await page.getByText('Kode selesai diproses.', { exact: true }).waitFor({ timeout: 20000 });
  await openSettings();
  const toolLogTable = page.locator('table.stats-table', { hasText: 'run_code' });
  await toolLogTable.first().waitFor({ timeout: 15000 });
  const logText = await toolLogTable.first().textContent();
  report.check('log audit tool mencatat run_code dari chat nyata',
    logText.includes('Plamo (M01)') && logText.includes('OK'), logText.slice(0, 120));
  await closeSettings();

  report.check('tanpa pageerror selama E2E depth settings', pageErrors.length === 0, pageErrors.join('; '));
} finally {
  await browser.close();
  await gateway.stop();
  await app.stop();
}
process.exit(report.summary() > 0 ? 1 : 0);
