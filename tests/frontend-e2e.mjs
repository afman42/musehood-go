// E2E frontend: alur pengguna nyata lewat UI Chromium terhadap binary
// asli + mock gateway — chat, mention, pengaturan (skill kursi), pin,
// ruang, pencarian, voting, dan persetujuan tool berbahaya.
// Jalankan: node tests/frontend-e2e.mjs
import { api, configureMockProvider, eventually, launchBrowser, makeReporter, startApp, startGateway } from './frontend-harness.mjs';

const report = makeReporter('FRONTEND E2E');
const app = await startApp({ skills: [{ name: 'contoh', description: 'Skill contoh uji', body: 'Ikuti contoh ini.' }, { name: 'gaya-singkat', description: 'Jawab singkat', body: 'Maksimal dua kalimat.' }] });
const gateway = await startGateway();
const browser = await launchBrowser();

try {
  await configureMockProvider(app.url, gateway.url, { M01: 'plain', M02: 'sse', M10: 'array' });
  const page = await browser.newPage({ viewport: { width: 1280, height: 800 } });
  const pageErrors = [];
  page.on('pageerror', e => pageErrors.push(e.message));
  await page.goto(app.url);
  await page.getByText('Pemilik Arman - Admin Muse - Hadir 8/8').waitFor();
  report.check('halaman utama termuat dengan 7 kartu kursi', await page.locator('.seat-card').count() === 7);

  const composer = page.getByRole('textbox', { name: 'Pesan untuk grup' });

  // 1. Mention lewat dialog @ Kursi.
  await page.getByRole('button', { name: '@ Kursi', exact: true }).click();
  await page.locator('[data-seat=M01]').click();
  report.check('dialog mention mengisi @Plamo', (await composer.inputValue()) === '@Plamo ');

  // 2. Kirim pesan -> balasan kursi tampil.
  await composer.fill('@Plamo halo e2e');
  await composer.press('Enter');
  await page.getByText('Balasan polos', { exact: true }).waitFor();
  report.check('balasan kursi tampil di chat', true);
  report.check('pesan pemilik berlabel Arman', (await page.locator('.message-owner .author-name').first().textContent()) === 'Arman');

  // 3. Pin pesan -> pinned bar muncul.
  await page.locator('.message').first().hover();
  await page.getByRole('button', { name: /pin|sematkan/i }).first().click().catch(() => {});
  const pinnedVisible = await eventually(async () => (await page.locator('.pinned-bar').count()) > 0, 3000).catch(() => false);
  report.check('pin pesan menampilkan pinned bar', pinnedVisible === true);

  // 4. Pengaturan: picker skill — cari, tekan, menempel sebagai chip.
  await page.getByRole('button', { name: 'Pengaturan grup', exact: true }).click();
  await page.locator('#settingsForm').waitFor({ state: 'visible' });
  const skillSearch = page.locator('[data-skill-search=M01]');
  await skillSearch.waitFor();
  await skillSearch.evaluate(el => {
    el.scrollIntoView({ block: 'center' });
    el.focus();
    document.execCommand('insertText', false, 'cont');
  });
  const m01Card = page.locator('.seat-settings', { has: page.locator('[data-skill-search=M01]') });
  await m01Card.getByRole('button', { name: 'Tambah skill contoh' }).click();
  await m01Card.locator('.skill-chip', { hasText: 'contoh' }).waitFor();
  report.check('skill menempel sebagai chip setelah ditekan', true);
  await page.locator('#saveSettings').click();
  await page.locator('#saveStatus').getByText('Tersimpan').waitFor();
  await eventually(async () => {
    const state = await api(app.url, 'GET', '/api/state');
    return state.seats.find(s => s.code === 'M01')?.skills?.includes('contoh');
  });
  report.check('skill terpilih tersimpan ke server', true);
  await page.getByRole('button', { name: 'Tutup pengaturan' }).click();

  // 5. Ruang baru: buat, pindah, chat kosong; kembali ke Utama utuh.
  await page.getByRole('button', { name: 'Buat ruang baru', exact: true }).click();
  await page.locator('#roomDialog').waitFor({ state: 'visible' });
  await page.locator('#roomDialog').getByLabel('Nama ruang baru').fill('Ruang E2E');
  await page.locator('#roomDialog').getByRole('button', { name: 'Buat ruang', exact: true }).click();
  await page.getByText('Ruang E2E').first().waitFor();
  const emptyCount = await page.locator('.message').count();
  report.check('ruang baru mulai kosong', emptyCount === 0, `pesan: ${emptyCount}`);
  await page.getByText('Utama', { exact: true }).first().click();
  await page.getByText('Balasan polos', { exact: true }).waitFor();
  report.check('kembali ke ruang Utama pesan utuh', true);

  // 6. Pencarian menemukan pesan.
  await page.getByRole('button', { name: /cari|pencarian/i }).first().click();
  const searchInput = page.getByRole('searchbox').or(page.locator('.search-bar input')).first();
  await searchInput.fill('halo e2e');
  const found = await eventually(async () => (await page.locator('.search-results, .server-results').count()) > 0, 5000).catch(() => false);
  report.check('pencarian menampilkan hasil', found === true);
  await page.keyboard.press('Escape');

  // 7. Voting jawaban terbaik.
  await composer.fill('@Plamo @GLM diskusi e2e');
  await composer.press('Enter');
  await page.getByText('Balasan SSE', { exact: true }).waitFor();
  await page.getByRole('button', { name: /voting|jawaban terbaik/i }).first().click();
  await page.getByText(/Hasil voting/).waitFor({ timeout: 15000 });
  report.check('voting menghasilkan ringkasan integrator', true);
  report.check('badge jawaban terbaik tampil', (await page.locator('.best-label').count()) > 0);

  // 8. Persetujuan tool berbahaya lewat banner UI.
  await api(app.url, 'POST', '/api/settings', { seats: [{ code: 'M01', model: 'toolcode' }] }, 200);
  await composer.fill('@Plamo jalankan kode uji');
  await composer.press('Enter');
  await page.getByRole('button', { name: 'Setujui' }).waitFor({ timeout: 15000 });
  report.check('banner persetujuan muncul untuk run_code berbahaya', true);
  await page.getByRole('button', { name: 'Tolak' }).click();
  await page.getByText('Kode selesai diproses.', { exact: true }).waitFor({ timeout: 15000 });
  report.check('penolakan persetujuan menyelesaikan balasan', true);
  const state = await api(app.url, 'GET', '/api/state');
  report.check('tidak ada persetujuan menggantung', (state.pendingApprovals ?? []).length === 0);

  report.check('tanpa pageerror selama E2E', pageErrors.length === 0, pageErrors.join('; '));
} finally {
  await browser.close();
  await gateway.stop();
  await app.stop();
}
process.exit(report.summary() > 0 ? 1 : 0);
