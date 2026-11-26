// E2E bagian Pengaturan yang belum ter-cover: CRUD provider (fetch/test
// models, edit alias, hapus), toggle diskusi/tool, kursi custom
// (tambah/hapus), kelola ruang, preset (simpan/terapkan/hapus),
// statistik, backup/restore, dan ekspor — semua lewat UI.
// Jalankan: node tests/frontend-e2e-settings.mjs
import { readFileSync } from 'node:fs';
import { api, configureMockProvider, eventually, launchBrowser, makeReporter, startApp, startGateway } from './frontend-harness.mjs';

const report = makeReporter('FRONTEND E2E PENGATURAN');
const app = await startApp();
const gateway = await startGateway();
const browser = await launchBrowser();

async function fillField(locator, value) {
  await locator.waitFor();
  await locator.evaluate((el, v) => {
    el.scrollIntoView({ block: 'center' });
    el.focus();
    document.execCommand('selectAll', false, null);
    document.execCommand('insertText', false, v);
  }, value);
}

try {
  const providerId = await configureMockProvider(app.url, gateway.url, { M01: 'plain' });
  const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
  page.on('dialog', d => d.accept());
  const pageErrors = [];
  page.on('pageerror', e => pageErrors.push(e.message));
  await page.goto(app.url);
  await page.getByText('Pemilik Arman - Admin Muse - Hadir 8/8').waitFor();

  // Satu pesan agar statistik terisi.
  const composer = page.getByRole('textbox', { name: 'Pesan untuk grup' });
  await composer.fill('@Plamo isi statistik');
  await composer.press('Enter');
  await page.getByText('Balasan polos', { exact: true }).waitFor();

  await page.getByRole('button', { name: 'Pengaturan grup', exact: true }).click();
  await page.locator('#settingsForm').waitFor({ state: 'visible' });

  // 1. Fetch Models + Tes Models pada baris provider Mock.
  const mockRow = page.locator('.provider-row', { hasText: 'Mock' }).first();
  await mockRow.getByRole('button', { name: 'Fetch Models' }).click();
  await mockRow.locator('.model-tag').first().waitFor();
  report.check('Fetch Models menampilkan daftar model', (await mockRow.locator('.model-tag').count()) >= 7);
  await mockRow.getByRole('button', { name: 'Tes Models' }).click();
  await mockRow.locator('.model-status').first().waitFor();
  report.check('Tes Models menampilkan hasil per model', (await mockRow.locator('.model-status').count()) >= 1);

  // 2. Edit provider: ganti alias.
  await mockRow.getByRole('button', { name: 'Edit', exact: true }).click();
  await fillField(page.getByLabel('Alias provider', { exact: true }), 'mk');
  await page.getByRole('button', { name: 'Simpan perubahan' }).click();
  await eventually(async () => {
    const providers = await api(app.url, 'GET', '/api/providers');
    return providers.providers.find(p => p.id === providerId)?.alias === 'mk';
  });
  report.check('edit alias provider tersimpan', true);

  // 3. Toggle tool + coding mati -> simpan -> nyala lagi.
  await page.locator('#toolsEnabled').uncheck();
  await page.locator('#codeToolEnabled').uncheck();
  await page.locator('#saveSettings').click();
  await page.locator('#saveStatus').getByText('Tersimpan').waitFor();
  let state = await api(app.url, 'GET', '/api/state');
  report.check('toggle tool/coding mati tersimpan', state.settings.toolsEnabled === false && state.settings.codeToolEnabled === false);
  await page.locator('#toolsEnabled').check();
  await page.locator('#codeToolEnabled').check();
  await page.locator('#saveSettings').click();
  await page.locator('#saveStatus').getByText('Tersimpan').waitFor();
  state = await api(app.url, 'GET', '/api/state');
  report.check('toggle tool/coding nyala tersimpan', state.settings.toolsEnabled !== false && state.settings.codeToolEnabled !== false);

  // 4. Kursi custom: tambah lalu hapus.
  await fillField(page.getByLabel('Nama kursi baru', { exact: true }), 'Nova');
  await fillField(page.getByLabel('Peran kursi baru', { exact: true }), 'Pengamat');
  await fillField(page.getByLabel('Model kursi baru', { exact: true }), 'plain');
  await page.getByRole('button', { name: 'Tambah kursi', exact: true }).click();
  const nova = await eventually(async () => {
    const s = await api(app.url, 'GET', '/api/state');
    return s.seats.find(x => x.name === 'Nova');
  });
  report.check('kursi custom Nova ditambahkan', Boolean(nova), nova?.code || '');
  await page.locator(`[data-seat-name="${nova.code}"]`).waitFor();
  const novaCard = page.locator('.seat-settings', { has: page.locator(`[data-seat-name="${nova.code}"]`) });
  await novaCard.getByRole('button', { name: 'Hapus kursi ini' }).click();
  await eventually(async () => {
    const s = await api(app.url, 'GET', '/api/state');
    return !s.seats.some(x => x.name === 'Nova');
  });
  report.check('kursi custom Nova dihapus', true);

  // 5. Kelola ruang di Pengaturan: tambah, ganti nama, hapus.
  await fillField(page.locator('.settings-section', { hasText: 'Ruang grup' }).getByLabel('Nama ruang baru'), 'Ruang Setelan');
  await page.getByRole('button', { name: 'Tambah ruang', exact: true }).click();
  const room = await eventually(async () => {
    const rooms = await api(app.url, 'GET', '/api/rooms');
    return rooms.rooms.find(r => r.name === 'Ruang Setelan');
  });
  report.check('ruang ditambah dari Pengaturan', Boolean(room));
  const roomCard = page.locator('.room-settings', { has: page.getByLabel(`Nama ruang ${room.id}`) });
  await fillField(roomCard.getByLabel(`Nama ruang ${room.id}`), 'Ruang Diganti');
  await roomCard.getByRole('button', { name: 'Simpan ruang' }).click();
  await eventually(async () => {
    const rooms = await api(app.url, 'GET', '/api/rooms');
    return rooms.rooms.some(r => r.name === 'Ruang Diganti');
  });
  report.check('nama ruang diganti dari Pengaturan', true);
  await roomCard.getByRole('button', { name: 'Hapus ruang' }).click();
  await eventually(async () => {
    const rooms = await api(app.url, 'GET', '/api/rooms');
    return !rooms.rooms.some(r => r.id === room.id);
  });
  report.check('ruang dihapus dari Pengaturan', true);

  // 6. Preset: simpan, ubah model via API, terapkan, hapus.
  await fillField(page.getByLabel('Nama preset baru', { exact: true }), 'Preset Uji');
  await page.getByRole('button', { name: 'Simpan preset dari susunan saat ini' }).click();
  await page.getByText('Preset Uji').first().waitFor();
  await api(app.url, 'POST', '/api/settings', { seats: [{ code: 'M01', model: 'sse' }] }, 200);
  await page.getByRole('button', { name: 'Terapkan', exact: true }).first().click();
  await eventually(async () => {
    const s = await api(app.url, 'GET', '/api/state');
    return s.seats.find(x => x.code === 'M01')?.model === 'plain';
  });
  report.check('preset diterapkan memulihkan model kursi', true);
  await page.locator('.provider-row', { hasText: 'Preset Uji' }).getByRole('button', { name: 'Hapus', exact: true }).click();
  await eventually(async () => {
    const presets = await api(app.url, 'GET', '/api/presets');
    return !presets.presets.some(p => p.name === 'Preset Uji');
  });
  report.check('preset dihapus', true);

  // 7. Statistik terisi.
  const statCells = await page.locator('.stats-table [role="cell"]').count();
  report.check('tabel statistik punya baris data', statCells >= 6, `${statCells} sel`);

  // 8. Backup: unduh, periksa tanpa API key, pulihkan kembali.
  const [backupDownload] = await Promise.all([
    page.waitForEvent('download'),
    page.getByRole('link', { name: 'Unduh cadangan' }).click(),
  ]);
  const backupPath = await backupDownload.path();
  const backup = JSON.parse(readFileSync(backupPath, 'utf8'));
  const leaked = JSON.stringify(backup).includes('browser-fixture-only-9876');
  report.check('cadangan tidak memuat API key', !leaked && backup.providers.every(p => !p.apiKey));
  await page.locator('#restoreInput').setInputFiles(backupPath);
  await page.getByText('Cadangan dipulihkan.').waitFor();
  report.check('cadangan dipulihkan lewat UI', true);

  // 9. Ekspor JSON.
  const [exportDownload] = await Promise.all([
    page.waitForEvent('download'),
    page.getByRole('link', { name: 'JSON', exact: true }).click(),
  ]);
  const exported = JSON.parse(readFileSync(await exportDownload.path(), 'utf8'));
  report.check('ekspor JSON memuat pesan', Array.isArray(exported.messages) && exported.messages.length > 0);

  // 10. Hapus provider kedua lewat UI.
  const second = await api(app.url, 'POST', '/api/providers', { name: 'Kedua', baseUrl: gateway.url + '/v1', apiKey: 'x', protocol: 'openai' }, 201);
  await page.reload();
  await page.getByText('Pemilik Arman - Admin Muse - Hadir 8/8').waitFor();
  await page.getByRole('button', { name: 'Pengaturan grup', exact: true }).click();
  await page.locator('#settingsForm').waitFor({ state: 'visible' });
  const secondRow = page.locator('.provider-row', { hasText: 'Kedua' }).first();
  await secondRow.getByRole('button', { name: 'Hapus', exact: true }).click();
  await eventually(async () => {
    const providers = await api(app.url, 'GET', '/api/providers');
    return !providers.providers.some(p => p.id === second.id);
  });
  report.check('provider kedua dihapus lewat UI', true);

  report.check('tanpa pageerror selama E2E pengaturan', pageErrors.length === 0, pageErrors.join('; '));
} finally {
  await browser.close();
  await gateway.stop();
  await app.stop();
}
process.exit(report.summary() > 0 ? 1 : 0);
