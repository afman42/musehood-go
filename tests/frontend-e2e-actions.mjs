// E2E aksi pesan lewat UI: edit, hapus, reaksi emoji, balas/kutip,
// unggah gambar, dan lampirkan file.
// Jalankan: node tests/frontend-e2e-actions.mjs
import { configureMockProvider, eventually, launchBrowser, makeReporter, startApp, startGateway } from './frontend-harness.mjs';
import { pixel } from './mock-gateway.mjs';

const report = makeReporter('FRONTEND E2E AKSI PESAN');
const app = await startApp();
const gateway = await startGateway();
const browser = await launchBrowser();

try {
  await configureMockProvider(app.url, gateway.url, { M01: 'plain' });
  const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
  page.on('dialog', d => d.accept());
  const pageErrors = [];
  page.on('pageerror', e => pageErrors.push(e.message));
  await page.goto(app.url);
  await page.getByText('Pemilik Arman - Admin Muse - Hadir 8/8').waitFor();
  const composer = page.getByRole('textbox', { name: 'Pesan untuk grup' });

  // Pesan awal pemilik + balasan kursi.
  await composer.fill('@Plamo pesan awal');
  await composer.press('Enter');
  await page.getByText('Balasan polos', { exact: true }).waitFor();
  const ownMessage = page.locator('.message-owner').first();

  // 1. Edit pesan sendiri.
  await ownMessage.hover();
  await ownMessage.getByRole('button', { name: 'Ubah pesan', exact: true }).click();
  const editBox = page.getByLabel('Ubah pesan', { exact: true }).last();
  await editBox.fill('@Plamo pesan awal (sudah diubah)');
  await page.locator('.edit-area').getByRole('button', { name: 'Simpan', exact: true }).click();
  await page.getByText('@Plamo pesan awal (sudah diubah)').waitFor();
  report.check('edit pesan sendiri tampil', true);

  // 2. Reaksi emoji pada balasan kursi.
  const seatMessage = page.locator('.message').filter({ hasText: 'Balasan polos' }).first();
  await seatMessage.hover();
  await seatMessage.getByRole('button', { name: 'Beri reaksi' }).click();
  await page.locator('.emoji-picker [role="menuitem"]').first().click();
  await eventually(async () => (await page.locator('.reaction-pill').count()) > 0);
  report.check('reaksi emoji tampil pada pesan', true);

  // 3. Balas pesan (kutip).
  await seatMessage.hover();
  await seatMessage.getByRole('button', { name: 'Balas pesan' }).click();
  await page.locator('.reply-chip').waitFor();
  await composer.fill('setuju dengan balasan itu');
  await composer.press('Enter');
  await page.locator('.quote-block').first().waitFor();
  report.check('balasan memuat blok kutipan', true);

  // 4. Unggah gambar lewat UI.
  await page.locator('#chatImageInput').setInputFiles({ name: 'uji.png', mimeType: 'image/png', buffer: pixel });
  await page.locator('.pending-image, .composer img').first().waitFor().catch(() => {});
  await page.locator('#sendButton').click();
  await page.waitForFunction(() => document.querySelector('.chat-image')?.naturalWidth === 1);
  report.check('gambar terunggah tampil di chat', true);

  // 5. Lampirkan file lewat UI.
  await page.locator('#chatFileInput').setInputFiles({ name: 'catatan.txt', mimeType: 'text/plain', buffer: Buffer.from('isi catatan uji') });
  await page.locator('#sendButton').click();
  await page.getByText(/catatan\.txt/).first().waitFor();
  report.check('lampiran file tampil di chat', true);

  // 6. Hapus pesan sendiri.
  const before = await page.locator('.message').count();
  const lastOwn = page.locator('.message-owner').last();
  await lastOwn.hover();
  await lastOwn.getByRole('button', { name: 'Hapus pesan' }).click();
  await eventually(async () => (await page.locator('.message').count()) < before);
  report.check('hapus pesan sendiri menghilangkan pesan', true);

  report.check('tanpa pageerror selama E2E aksi', pageErrors.length === 0, pageErrors.join('; '));
} finally {
  await browser.close();
  await gateway.stop();
  await app.stop();
}
process.exit(report.summary() > 0 ? 1 : 0);
