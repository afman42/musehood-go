// E2E frontend untuk fitur pendalaman 27 Nov 2026: reaksi toggle,
// ekspor diskusi, fork, rerun, debat 5 tahap, urutan bicara ruang,
// arsip ruang, shortcut keyboard, dan layar login PIN.
// Jalankan: node tests/frontend-e2e-depth.mjs
import { api, configureMockProvider, eventually, launchBrowser, makeReporter, startApp, startGateway } from './frontend-harness.mjs';

const report = makeReporter('FRONTEND E2E DEPTH');
const app = await startApp();
const gateway = await startGateway();
const browser = await launchBrowser();

try {
  await configureMockProvider(app.url, gateway.url, { M01: 'plain', M02: 'sse', M10: 'array' });
  // Integrator gateway agar tahap kesimpulan debat selesai tanpa pekerja antrean.
  await api(app.url, 'POST', '/api/settings', { settings: { integratorSeat: 'M10' } }, 200);
  const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
  page.on('dialog', d => d.accept());
  const pageErrors = [];
  page.on('pageerror', e => pageErrors.push(e.message));
  await page.goto(app.url);
  await page.getByText('Pemilik Arman - Admin Muse - Hadir 8/8').waitFor();
  const composer = page.getByRole('textbox', { name: 'Pesan untuk grup' });

  // Pesan awal untuk reaksi/ekspor/fork/rerun.
  await composer.fill('@Plamo pesan depth');
  await composer.press('Enter');
  await page.getByText('Balasan polos', { exact: true }).waitFor();
  const seatMessage = page.locator('.message').filter({ hasText: 'Balasan polos' }).first();

  // 1. Reaksi toggle: tambah -> pil tampil; tekan pil -> hilang.
  await seatMessage.hover();
  await seatMessage.getByRole('button', { name: 'Beri reaksi' }).click();
  await page.locator('.emoji-picker [role="menuitem"]', { hasText: '👍' }).click();
  await page.locator('.reaction-pill', { hasText: '👍 1' }).waitFor();
  report.check('reaksi 👍 tampil dengan hitungan 1', true);
  await page.locator('.reaction-pill', { hasText: '👍' }).click();
  await eventually(async () => (await page.locator('.reaction-pill').count()) === 0);
  report.check('tekan pil reaksi lagi menghapus reaksi (toggle)', true);

  // 2. Ekspor diskusi: tombol membuka /api/export (popup) yang mengunduh
  // diskusi-musehood.html. Request didengarkan di level context karena
  // window.open membuat halaman popup terpisah.
  const exportUrls = [];
  page.context().on('request', req => {
    if (req.url().includes('/api/export?message=')) exportUrls.push(req.url());
  });
  await seatMessage.hover();
  await seatMessage.getByRole('button', { name: 'Ekspor diskusi ini' }).click();
  await eventually(async () => exportUrls.length > 0);
  const exportRes = await page.request.get(exportUrls[0]);
  const disposition = exportRes.headers()['content-disposition'] || '';
  report.check('ekspor diskusi mengunduh diskusi-musehood.html',
    exportRes.ok() && disposition.includes('diskusi-musehood.html'), disposition);

  // 3. Fork: ruang baru aktif dengan riwayat tersalin.
  const ownerMessage = page.locator('.message-owner').first();
  await ownerMessage.hover();
  await ownerMessage.getByRole('button', { name: 'Fork ke ruang baru' }).click();
  await eventually(async () => {
    const active = await page.locator('.room-chip.active').textContent();
    return active?.startsWith('Fork:');
  });
  // Fork dari pesan pemilik pertama menyalin riwayat sampai pesan itu saja.
  const forkMessages = await page.locator('.message').count();
  const forkText = await page.locator('.message').first().textContent();
  report.check('fork membuat ruang baru aktif dengan riwayat tersalin sampai titik fork',
    forkMessages === 1 && forkText.includes('pesan depth'), `pesan: ${forkMessages}`);
  await page.locator('.room-chip', { hasText: 'Utama' }).first().click();
  await page.getByText('Balasan polos', { exact: true }).first().waitFor();

  // 4. Rerun dari pesan pemilik pertama: pesan sesudahnya diganti balasan baru.
  await composer.fill('@Plamo pesan sesudah rerun');
  await composer.press('Enter');
  await page.getByText('Balasan polos', { exact: true }).nth(1).waitFor();
  const firstOwner = page.locator('.message-owner').first();
  await firstOwner.hover();
  await firstOwner.getByRole('button', { name: 'Jalankan ulang dari sini' }).click();
  await eventually(async () => (await page.getByText('pesan sesudah rerun').count()) === 0, 15000);
  await page.getByText('Balasan polos', { exact: true }).first().waitFor();
  report.check('rerun menghapus pesan sesudahnya dan membuat balasan baru', true);

  // 5. Debat: dialog -> 5 tahap berlabel berurutan.
  await page.locator('#debateButton').click();
  const debate = page.locator('#debateDialog');
  await debate.waitFor({ state: 'visible' });
  await debate.locator('select').nth(0).selectOption('M01');
  await debate.locator('select').nth(1).selectOption('M02');
  await debate.locator('textarea').fill('Kopi lebih baik dari teh');
  await debate.getByRole('button', { name: 'Mulai Debat' }).click();
  await page.locator('.final-label', { hasText: 'Debat — Kesimpulan' }).waitFor({ timeout: 30000 });
  const labels = await page.locator('.final-label').allTextContents();
  report.check('debat 5 tahap berurutan (Pro/Kontra/Sanggahan/Sanggahan/Kesimpulan)',
    JSON.stringify(labels) === JSON.stringify(['Debat — Pro', 'Debat — Kontra', 'Debat — Sanggahan', 'Debat — Sanggahan', 'Debat — Kesimpulan']),
    labels.join(' | '));
  const conclusion = page.locator('.message', { has: page.locator('.final-label', { hasText: 'Kesimpulan' }) });
  await conclusion.getByText('Balasan array', { exact: true }).waitFor();
  report.check('kesimpulan debat dijawab kursi integrator (M10)', true);

  // 6. Urutan bicara ruang: M02 sebelum M01 untuk pesan biasa.
  const orderRoom = await api(app.url, 'POST', '/api/rooms', { name: 'Ruang Urutan', seatCodes: ['M01', 'M02'], seatOrder: ['M02', 'M01'] }, 201);
  const orderRoomId = orderRoom.id ?? orderRoom.room?.id;
  const stateAfterCreate = await api(app.url, 'GET', '/api/state');
  const orderId = orderRoomId ?? stateAfterCreate.rooms.find(r => r.name === 'Ruang Urutan')?.id;
  await api(app.url, 'POST', `/api/rooms/${orderId}/activate`, {}, 200);
  await page.reload();
  await page.getByText('Pemilik Arman - Admin Muse - Hadir 8/8').waitFor();
  await composer.fill('halo urutan');
  await composer.press('Enter');
  await page.getByText('Balasan SSE', { exact: true }).waitFor();
  await page.getByText('Balasan polos', { exact: true }).waitFor();
  const replyTexts = await page.locator('.message:not(.message-owner) .message-text').allTextContents();
  report.check('urutan bicara ruang diikuti (GLM sebelum Plamo)',
    replyTexts[0] === 'Balasan SSE' && replyTexts[1] === 'Balasan polos', replyTexts.join(' | '));

  // 7. Arsip ruang: hilang dari bilah, pulihkan dari pengaturan.
  const archiveRoom = await api(app.url, 'POST', '/api/rooms', { name: 'Ruang Arsip Uji', seatCodes: [] }, 201);
  const stateRooms = await api(app.url, 'GET', '/api/state');
  const archiveId = archiveRoom.id ?? stateRooms.rooms.find(r => r.name === 'Ruang Arsip Uji')?.id;
  const utama = stateRooms.rooms.find(r => r.name === 'Utama');
  await api(app.url, 'POST', `/api/rooms/${utama.id}/activate`, {}, 200);
  await page.reload();
  await page.getByText('Pemilik Arman - Admin Muse - Hadir 8/8').waitFor();
  await page.locator('.room-chip', { hasText: 'Ruang Arsip Uji' }).waitFor();
  await page.getByRole('button', { name: 'Pengaturan grup', exact: true }).click();
  await page.locator('#settingsForm').waitFor({ state: 'visible' });
  const archiveArticle = page.locator('.room-settings', { has: page.getByLabel(`Nama ruang ${archiveId}`) });
  await archiveArticle.getByRole('button', { name: 'Arsipkan', exact: true }).click();
  await eventually(async () => {
    const state = await api(app.url, 'GET', '/api/state');
    return state.rooms.find(r => r.id === archiveId)?.archived === true;
  });
  await page.getByRole('button', { name: 'Tutup pengaturan' }).click();
  await eventually(async () => (await page.locator('.room-chip', { hasText: 'Ruang Arsip Uji' }).count()) === 0);
  report.check('ruang terarsip hilang dari bilah ruang', true);
  await page.getByRole('button', { name: 'Pengaturan grup', exact: true }).click();
  await page.locator('#settingsForm').waitFor({ state: 'visible' });
  await archiveArticle.getByRole('button', { name: 'Pulihkan', exact: true }).click();
  await eventually(async () => {
    const state = await api(app.url, 'GET', '/api/state');
    const room = state.rooms.find(r => r.id === archiveId);
    // Field archived bersifat omitempty: tidak ada berarti tidak terarsip.
    return room && !room.archived;
  });
  await page.getByRole('button', { name: 'Tutup pengaturan' }).click();
  await page.locator('.room-chip', { hasText: 'Ruang Arsip Uji' }).waitFor();
  report.check('ruang dipulihkan tampil lagi di bilah ruang', true);

  // 8. Shortcut keyboard: Ctrl+K cari, ? bantuan, Alt+2 ruang, Ctrl+/ fokus, Ctrl+Enter kirim.
  await page.keyboard.press('Control+k');
  await page.locator('.search-bar').waitFor({ state: 'visible' });
  report.check('Ctrl+K membuka pencarian', true);
  await page.locator('.search-bar').getByRole('button', { name: /tutup/i }).first().click();
  await page.locator('header.group-header').click();
  await page.keyboard.press('?');
  await page.locator('#shortcutHelpDialog').waitFor({ state: 'visible' });
  report.check('tombol ? membuka bantuan shortcut', true);
  await page.keyboard.press('Escape');
  await eventually(async () => (await page.locator('#shortcutHelpDialog[open]').count()) === 0);
  report.check('Escape menutup dialog bantuan', true);
  await page.keyboard.press('Alt+2');
  await eventually(async () => {
    const state = await api(app.url, 'GET', '/api/state');
    return state.activeRoomId !== utama.id;
  });
  report.check('Alt+2 berpindah ke ruang kedua', true);
  // Tunggu UI selesai berpindah sebelum Alt+1 (menghindari race state basi).
  await eventually(async () => {
    const active = await page.locator('.room-chip.active').textContent();
    return active && !active.includes('Utama');
  });
  await page.keyboard.press('Alt+1');
  await eventually(async () => {
    const state = await api(app.url, 'GET', '/api/state');
    return state.activeRoomId === utama.id;
  });
  await page.keyboard.press('Control+/');
  const focused = await page.evaluate(() => document.activeElement?.getAttribute('aria-label'));
  report.check('Ctrl+/ memfokuskan composer', focused === 'Pesan untuk grup', String(focused));
  await composer.fill('@Plamo via ctrl enter');
  await page.keyboard.press('Control+Enter');
  await page.getByText('Balasan polos', { exact: true }).last().waitFor();
  report.check('Ctrl+Enter mengirim pesan', true);

  report.check('tanpa pageerror selama E2E depth', pageErrors.length === 0, pageErrors.join('; '));
} finally {
  await browser.close();
  await gateway.stop();
  await app.stop();
}

// 9. PIN: app terpisah agar gerbang PIN tidak mengganggu alur di atas.
const pinApp = await startApp();
const pinGateway = await startGateway();
const pinBrowser = await launchBrowser();
try {
  await configureMockProvider(pinApp.url, pinGateway.url, { M01: 'plain' });
  const page = await pinBrowser.newPage({ viewport: { width: 1280, height: 900 } });
  const pinErrors = [];
  page.on('pageerror', e => pinErrors.push(e.message));
  await page.goto(pinApp.url);
  await page.getByText('Pemilik Arman - Admin Muse - Hadir 8/8').waitFor();

  // Pasang PIN lewat pengaturan.
  await page.getByRole('button', { name: 'Pengaturan grup', exact: true }).click();
  await page.locator('#settingsForm').waitFor({ state: 'visible' });
  await page.getByPlaceholder('4–64 karakter').fill('1234');
  await page.getByRole('button', { name: 'Pasang PIN', exact: true }).click();
  await page.getByText('PIN tersimpan.').waitFor();
  report.check('PIN terpasang lewat pengaturan', true);
  await page.getByRole('button', { name: 'Tutup pengaturan' }).click();

  // Sesi baru (konteks bersih) -> layar login.
  const fresh = await pinBrowser.newContext({ viewport: { width: 1280, height: 900 } });
  const loginPage = await fresh.newPage();
  await loginPage.goto(pinApp.url);
  await loginPage.locator('.login-card').waitFor({ state: 'visible' });
  report.check('layar login PIN tampil untuk sesi baru', true);
  await loginPage.locator('.login-card input[type="password"]').fill('9999');
  await loginPage.getByRole('button', { name: 'Masuk', exact: true }).click();
  await loginPage.getByText('PIN salah.').waitFor();
  report.check('PIN salah ditolak dengan pesan', true);
  await loginPage.locator('.login-card input[type="password"]').fill('1234');
  await loginPage.getByRole('button', { name: 'Masuk', exact: true }).click();
  await loginPage.getByText('Pemilik Arman - Admin Muse - Hadir 8/8').waitFor();
  report.check('PIN benar masuk ke grup', true);

  // Logout dari pengaturan -> kembali ke layar login.
  await loginPage.getByRole('button', { name: 'Pengaturan grup', exact: true }).click();
  await loginPage.locator('#settingsForm').waitFor({ state: 'visible' });
  await loginPage.getByRole('button', { name: 'Keluar (logout)', exact: true }).click();
  await loginPage.locator('.login-card').waitFor({ state: 'visible' });
  report.check('logout mengembalikan layar login', true);
  report.check('tanpa pageerror selama alur PIN', pinErrors.length === 0, pinErrors.join('; '));
  await fresh.close();
} finally {
  await pinBrowser.close();
  await pinGateway.stop();
  await pinApp.stop();
}
process.exit(report.summary() > 0 ? 1 : 0);
